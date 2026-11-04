package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
)

// executionController is the live authority for one execution, independent of
// whether a terminal client or a detached supervisor owns its inherited I/O.
// The private endpoint and lease are the existing managed-process protocol.
type executionController struct {
	store          Store
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	record         Record
	probe          func(context.Context) Readiness
	probeGate      chan struct{}
	server         *http.Server
	stopValueWatch func()
}

func (s Store) newController(ctx context.Context, r Record) (*executionController, error) {
	r.Cleanup = initialCleanup()
	ctx, cancel := context.WithCancel(ctx)
	c := &executionController{store: s, ctx: ctx, cancel: cancel, record: r, probeGate: make(chan struct{}, 1), stopValueWatch: func() {}}
	c.probeGate <- struct{}{}
	listener, err := ports.ListenTCP(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		return nil, err
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		cancel()
		listener.Close()
		return nil, err
	}
	endpoint := control{ID: r.ID, Address: "http://" + listener.Addr().String(), Token: hex.EncodeToString(token[:])}
	c.server = &http.Server{ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 35 * time.Second, MaxHeaderBytes: 8192}
	c.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if q.Host != strings.TrimPrefix(endpoint.Address, "http://") || q.Header.Get("Origin") != "" || q.Method != "POST" || q.Header.Get("Authorization") != "Bearer "+endpoint.Token {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if q.URL.Path != "/status" && q.URL.Path != "/stop" && q.URL.Path != "/check" {
			http.NotFound(w, q)
			return
		}
		c.mu.Lock()
		snapshot, check := c.record, c.probe
		c.mu.Unlock()
		if q.URL.Path == "/check" && snapshot.State == "running" && check != nil {
			select {
			case <-q.Context().Done():
				return
			case <-ctx.Done():
				return
			case <-c.probeGate:
			}
			defer func() { c.probeGate <- struct{}{} }()
			if ctx.Err() != nil {
				return
			}
			query, cancelQuery := context.WithCancel(q.Context())
			stop := context.AfterFunc(ctx, cancelQuery)
			result := check(query)
			stop()
			cancelQuery()
			c.mu.Lock()
			snapshot = c.record
			c.mu.Unlock()
			if ctx.Err() != nil || snapshot.EndedAt != nil {
				result.Ready, result.Reason = false, "process_stopped"
			}
			snapshot.Readiness = &result
		}
		w.Header().Set("Content-Type", "application/json")
		snapshot.controlObserved = true
		_ = json.NewEncoder(w).Encode(snapshot)
		if q.URL.Path == "/stop" {
			c.cancel()
		}
	})
	if err := writePrivate(s.path(r.ID, "control.json"), endpoint); err != nil {
		cancel()
		listener.Close()
		return nil, err
	}
	go func() { _ = c.server.Serve(listener) }()
	return c, nil
}

func (c *executionController) close() {
	c.cancel()
	_ = c.server.Close()
	<-c.probeGate
	c.stopValueWatch()
}

func (c *executionController) run(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	ctx = process.WithCleanupStarted(ctx, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.record.Cleanup.State, c.record.Cleanup.Reason = "pending", "owned_processes_stopping"
		c.record.State = "stopping"
	})
	return process.ExecuteOwned(ctx, args, dir, env, in, out, diagnostic, func() *protocol.Error {
		now := time.Now().UTC()
		c.mu.Lock()
		next := c.record
		if selection, ok := ctx.Value(environmentSelectionKey{}).(environmentSelection); ok {
			next.Env, next.Inject = selection.Env, &selection.Inject
		}
		config := process.ReadyProbe(ctx)
		next.ReadyConfigured = config != nil
		next.StartedAt, next.State = &now, "running"
		if next.InputSnapshotID != "" {
			next.LaunchPhase = "spawned"
		}
		if err := writePrivate(c.store.path(next.ID, "record.json"), next); err != nil {
			c.mu.Unlock()
			return storageErrorAt("write_execution_record", err)
		}
		c.record = next
		if config != nil {
			preparedEnv := append([]string{}, env...)
			c.probe = func(query context.Context) Readiness { return runProbe(query, *config, dir, preparedEnv) }
		}
		c.mu.Unlock()
		if next.Mode == "managed" {
			c.stopValueWatch = c.store.startValueWatch(ctx, func() Record { c.mu.Lock(); defer c.mu.Unlock(); return c.record }, func(status ValueWatchStatus) {
				c.mu.Lock()
				defer c.mu.Unlock()
				if c.record.EndedAt == nil {
					c.record.ValueWatch = &status
				}
			})
		}
		return nil
	})
}

func (c *executionController) finish(code int, condition *protocol.Error) error {
	stopped := c.ctx.Err() != nil
	// Readiness probes own separate process groups/Jobs. Finish them before
	// terminal publication, while retaining the application's actual outcome.
	c.cancel()
	<-c.probeGate
	c.probeGate <- struct{}{}
	c.mu.Lock()
	defer c.mu.Unlock()
	r := &c.record
	c.store.finishInputPhase(r)
	now := time.Now().UTC()
	r.EndedAt, r.State, r.ExitCode = &now, "exited", &code
	r.Cleanup.State, r.Cleanup.Reason = "complete", "owned_scope_released"
	if condition != nil && (condition.Code == "process_cleanup_incomplete" || condition.Code == "launch_outcome_unknown") {
		r.Cleanup.State, r.Cleanup.Reason = "unknown", condition.Code
	}
	if condition != nil && (condition.Code == "launch_outcome_unknown" || condition.Code == "output_protection_failed" || condition.Code == "process_cleanup_incomplete") {
		r.State, r.Reason, r.ExitCode = "failed", condition.Code, nil
	} else if stopped {
		r.State, r.Reason = "stopped", "stop_requested"
	} else if condition != nil {
		r.State, r.Reason, r.ExitCode = "failed", condition.Code, nil
	} else if code != 0 {
		r.Reason = "nonzero_exit"
	}
	return writePrivate(c.store.path(r.ID, "record.json"), *r)
}

func (s Store) serveExecution(ctx context.Context, id string, inherited *os.File, execute Execute) error {
	if !validID(id) {
		return errors.New("invalid ID")
	}
	_, hasMarker, markerErr := s.readLaunchMarker(id)
	if markerErr != nil {
		return markerErr
	}
	var lease *lockedLease
	if hasMarker {
		if inherited == nil {
			return errors.New("inherited lease required")
		}
		var err error
		lease, err = adoptInheritedLease(inherited, s.leasePath(id))
		if err != nil {
			return err
		}
	} else {
		if inherited != nil {
			return errors.New("unexpected inherited lease")
		}
		var acquired bool
		var err error
		lease, acquired, err = tryLease(s.leasePath(id), true)
		if err != nil || !acquired {
			return errors.New("lease unavailable")
		}
	}
	defer lease.unlockClose()
	r, condition := s.read(id)
	if condition != nil || r.EndedAt != nil {
		return errors.New("invalid execution")
	}
	r.Mode = "managed"
	c, err := s.newController(ctx, r)
	if err != nil {
		return err
	}
	defer c.close()
	var output io.Writer = io.Discard
	if r.Capture {
		output = &boundedLog{path: s.path(id, "output.log")}
	}
	code := 0
	runner := func(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
		if condition := process.ValidateManaged(args, dir, env); condition != nil {
			return 0, condition
		}
		var condition *protocol.Error
		code, condition = c.run(ctx, args, dir, env, in, out, diagnostic)
		return code, condition
	}
	condition = execute(c.ctx, r, runner, output)
	return c.finish(code, condition)
}

// RunForeground uses the same execution records, lease, control and completion
// rules as detached runs, while preserving the caller's streams and terminal.
// Callers decide whether tracking is required before invoking this method.
func (s Store) RunForeground(ctx context.Context, prepared execution.Prepared, parent []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	command := prepared.Command
	r := Record{ID: command.ExecutionID, Mode: "foreground", Profile: command.Project.Profile, Directory: command.Project.Root, Command: command.Name, Env: command.Env, Inject: &command.Inject, Selection: command.Selection, IsolatedRoot: s.IsolatedRoot, CreatedAt: time.Now().UTC(), State: "starting"}
	r.Cleanup = initialCleanup()
	if len(prepared.Assignments) > 0 {
		r.Instance = prepared.Assignments[0].Instance.ID
	}
	if prepared.Input != nil {
		metadata := prepared.Input.Metadata()
		r.InputVersion = metadata.Delivery.Clone()
	}
	lease, condition := s.prepareLaunchContext(ctx, r)
	if condition != nil {
		if condition.Details == nil {
			condition.Details = map[string]any{}
		}
		condition.Details["phase"] = "register_execution"
		return 0, condition
	}
	defer lease.unlockClose()
	c, err := s.newController(ctx, r)
	if err != nil {
		now := time.Now().UTC()
		r.EndedAt, r.State, r.Reason = &now, "failed", "control_start_failed"
		_ = writePrivate(s.path(r.ID, "record.json"), r)
		condition := storageErrorAt("start_execution_control", err)
		condition.Details["phase"] = "register_execution"
		return 0, condition
	}
	defer c.close()
	code, condition := prepared.Execute(process.WithRunner(c.ctx, c.run), parent, nil, in, out, diagnostic)
	// Release service claims before publishing terminal completion. The caller
	// supplies a once-protected Release for preparation and startup failures too.
	prepared.Release()
	if err := c.finish(code, condition); err != nil && condition == nil {
		condition = storageErrorAt("finish_foreground_execution", err)
	}
	return code, condition
}
