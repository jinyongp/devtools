// Package runnerengine is the caller-owned reference engine used by the example
// receiver. Its private ledger stores metadata only and never retries a reserved
// job after restart. It is intentionally separate from devtools' value store.
package runnerengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/valuehandoff"
)

var ErrUnknown = errors.New("engine outcome unavailable; do not resubmit")

const ledgerMarker = "devtools-reference-engine-v1\n"

type Result struct {
	Job       string `json:"external_execution_id"`
	Input     string `json:"input_id"`
	Execution string `json:"execution_id"`
	Delivery  string `json:"delivery_id"`
	Ref       string `json:"external_ref"`
	State     string `json:"state"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type Engine struct {
	Directory string
	Program   string
	Arguments []string
	claims    sync.Map
	hook      func(string)
}

func jobKey(job string) string { h := sha256.Sum256([]byte(job)); return hex.EncodeToString(h[:]) }

// Initialize is an explicit caller setup action. Normal operation never creates
// a missing ledger: losing the deduplication history must fail closed.
func (e *Engine) Initialize(ctx context.Context) error {
	root, err := platformfs.OpenRoot(e.Directory, true)
	if err != nil {
		return err
	}
	defer root.Close()
	release, err := maintenance.LockExclusive(ctx, filepath.Join(e.Directory, "initialize.lock"))
	if err != nil {
		return err
	}
	defer release()
	b, err := root.ReadFile("ledger.version", 64)
	if err == nil {
		if string(b) == ledgerMarker {
			return nil
		}
		return ErrUnknown
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return root.WriteFile(ctx, "ledger.version", []byte(ledgerMarker), false)
}

func (e *Engine) locked(ctx context.Context, job string, fn func(*platformfs.Root, string) error) error {
	root, err := platformfs.OpenRoot(e.Directory, false)
	if err != nil {
		return ErrUnknown
	}
	defer root.Close()
	if b, err := root.ReadFile("ledger.version", 64); err != nil || string(b) != ledgerMarker {
		return ErrUnknown
	}
	key := jobKey(job)
	release, err := maintenance.LockExclusive(ctx, filepath.Join(e.Directory, key+".lock"))
	if err != nil {
		return err
	}
	defer release()
	return fn(root, key+".json")
}
func read(root *platformfs.Root, path string) (Result, error) {
	b, err := root.ReadFile(path, 16384)
	if err != nil {
		return Result{}, err
	}
	var r Result
	if valuehandoff.DecodeControl(b, &r) != nil || r.Job == "" || r.Input == "" || r.Ref != jobKey(r.Job) {
		return Result{}, ErrUnknown
	}
	return r, nil
}
func write(ctx context.Context, root *platformfs.Root, path string, r Result, replace bool) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return root.WriteFile(ctx, path, b, replace)
}

// Accept durably reserves the external job before acknowledging any input. Only
// this process' one-use claim may start it; a restart cannot recreate the claim.
func (e *Engine) Accept(ctx context.Context, input *valuehandoff.ReceivedInput) error {
	execution, selected, job, delivery := input.Identity()
	return e.reserve(ctx, execution, selected, job, delivery)
}
func (e *Engine) reserve(ctx context.Context, execution, selected, job, delivery string) error {
	if !filepath.IsAbs(e.Program) || job == "" {
		return ErrUnknown
	}
	return e.locked(ctx, job, func(root *platformfs.Root, path string) error {
		if _, err := read(root, path); !errors.Is(err, os.ErrNotExist) {
			return ErrUnknown
		}
		r := Result{Job: job, Input: selected, Execution: execution, Delivery: delivery, Ref: jobKey(job), State: "unknown", Reason: "outcome_unavailable"}
		if err := write(ctx, root, path, r, false); err != nil {
			return err
		}
		e.claims.Store(delivery, r)
		return nil
	})
}

// Handle starts the helper at most once and waits until its protected streams
// reach EOF. Selected values cross only the dedicated child environment boundary.
func (e *Engine) Handle(ctx context.Context, input *valuehandoff.ReceivedInput, out, diagnostic io.Writer) error {
	_, selected, job, delivery := input.Identity()
	return e.run(ctx, selected, job, delivery, input.Environment(), out, diagnostic)
}
func (e *Engine) run(ctx context.Context, selected, job, delivery string, environment []string, out, diagnostic io.Writer) error {
	claim, ok := e.claims.LoadAndDelete(delivery)
	if !ok {
		return ErrUnknown
	}
	r := claim.(Result)
	if r.Job != job || r.Input != selected {
		return ErrUnknown
	}
	err := e.locked(ctx, job, func(root *platformfs.Root, path string) error {
		current, err := read(root, path)
		if err != nil || current.Delivery != delivery || current.State != "unknown" {
			return ErrUnknown
		}
		return nil
	})
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, e.Program, e.Arguments...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Env, cmd.Stdout, cmd.Stderr = environment, out, diagnostic
	if e.hook != nil {
		e.hook("reserved")
	}
	if err := cmd.Start(); err != nil {
		r.State, r.Reason = "failed", "runner_start_failed"
		return e.publish(ctx, r)
	}
	if e.hook != nil {
		e.hook("started")
	}
	// A crash here leaves the pre-start reservation unknown. Recovery never
	// infers completion from a PID or submits replacement work.
	err = cmd.Wait()
	if cmd.ProcessState == nil {
		return ErrUnknown
	}
	code := cmd.ProcessState.ExitCode()
	if code < 0 || ctx.Err() != nil || errors.Is(err, exec.ErrWaitDelay) {
		return ErrUnknown
	}
	r.State, r.Reason, r.ExitCode = "finished", "", &code
	if err != nil {
		r.Reason = "execution_failed"
	}
	return e.publish(ctx, r)
}
func (e *Engine) publish(ctx context.Context, r Result) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return e.locked(ctx, r.Job, func(root *platformfs.Root, path string) error {
		current, err := read(root, path)
		if err != nil || current.Delivery != r.Delivery || current.Input != r.Input || current.State != "unknown" {
			return ErrUnknown
		}
		return write(ctx, root, path, r, true)
	})
}

// Lookup returns only durable engine facts. An absent or reserved job cannot be
// promoted to a new execution; callers must keep that outcome unresolved.
func (e *Engine) Lookup(ctx context.Context, job string) (Result, error) {
	var result Result
	err := e.locked(ctx, job, func(root *platformfs.Root, path string) error {
		var err error
		result, err = read(root, path)
		if err != nil || result.Job != job {
			return ErrUnknown
		}
		return nil
	})
	return result, err
}
