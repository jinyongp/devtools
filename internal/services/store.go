// Package services manages detached, named project commands through authenticated supervisors.
package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Store struct{ Data string }

func writePrivate(path string, v any) error {
	if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
		return e
	}
	return tasks.WritePrivate(path, v)
}

type Record struct {
	ReadyConfigured bool       `json:"ready_configured"`
	Readiness       *Readiness `json:"readiness,omitempty"`
	ID              string     `json:"id"`
	Profile         string     `json:"profile"`
	Instance        string     `json:"instance_id"`
	Directory       string     `json:"directory"`
	Command         string     `json:"command"`
	Env             string     `json:"env"`
	EnvOverride     *string    `json:"env_override"`
	Capture         bool       `json:"capture_logs"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	ExitCode        *int       `json:"exit_code"`
	Reason          string     `json:"reason"`
	State           string     `json:"state"`
	Previous        string     `json:"previous_id,omitempty"`
}
type Request struct {
	Action    string  `json:"action"`
	ID        string  `json:"id,omitempty"`
	Directory string  `json:"directory,omitempty"`
	Command   string  `json:"command,omitempty"`
	Env       *string `json:"env,omitempty"`
	Capture   *bool   `json:"capture_logs,omitempty"`
	RequestID string  `json:"request_id"`
}
type Result struct {
	Item     Record `json:"item"`
	Changed  bool   `json:"changed"`
	Replayed bool   `json:"replayed"`
}
type receipt struct {
	Fingerprint string `json:"fingerprint"`
	Result      Result `json:"result"`
	Done        bool   `json:"done"`
}
type control struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Token   string `json:"token"`
}

func failure(code string) *protocol.Error {
	return protocol.NewError(code, "Process operation could not satisfy the requested condition.", 3, nil)
}
func storageError() *protocol.Error {
	return protocol.NewError("io_error", "Cannot access private process storage.", 1, nil)
}
func validID(s string) bool {
	b, e := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return e == nil && len(b) == 16 && len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
func (s Store) root() string                { return filepath.Join(s.Data, "processes") }
func (s Store) path(id, name string) string { return filepath.Join(s.root(), id, name) }
func (s Store) read(id string) (Record, *protocol.Error) {
	var r Record
	if !validID(id) {
		return r, failure("invalid_argument")
	}
	if e := tasks.ReadPrivate(s.path(id, "record.json"), &r); e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return r, failure("process_not_found")
		}
		return r, storageError()
	}
	if r.ID != id || !project.ValidProfile(r.Profile) || !filepath.IsAbs(r.Directory) {
		return r, storageError()
	}
	return r, nil
}
func (s Store) rpc(ctx context.Context, id, action string) (Record, error) {
	var c control
	var out Record
	if tasks.ReadPrivate(s.path(id, "control.json"), &c) != nil || c.ID != id || !strings.HasPrefix(c.Address, "http://127.0.0.1:") || len(c.Token) != 64 {
		return out, errors.New("unavailable")
	}
	u, parseErr := url.Parse(c.Address)
	if parseErr != nil {
		return out, errors.New("invalid address")
	}
	port, parseErr := strconv.Atoi(u.Port())
	if parseErr != nil || port < 1 || port > 65535 || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return out, errors.New("invalid address")
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.Address+"/"+action, nil)
	if e != nil {
		return out, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	timeout := time.Second
	if action == "check" {
		timeout = 35 * time.Second
	}
	client := http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, e := client.Do(req)
	if e != nil {
		return out, e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return out, errors.New("unavailable")
	}
	e = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&out)
	if out.ID != id {
		return out, errors.New("identity mismatch")
	}
	return out, e
}
func (s Store) Status(ctx context.Context, id string) (Record, *protocol.Error) {
	r, e := s.read(id)
	if e != nil {
		return r, e
	}
	if r.EndedAt != nil {
		return r, nil
	}
	if live, e := s.rpc(ctx, id, "status"); e == nil {
		return live, nil
	}
	r.State = "unknown"
	r.Reason = "supervisor_unavailable"
	if s.leaseFree(ctx, id) {
		r.State = "interrupted"
		r.Reason = "supervisor_lost"
	}
	return r, nil
}
func (s Store) leaseFree(ctx context.Context, id string) bool {
	wait, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	release, e := tasks.Lock(wait, s.path(id, "lease.lock"))
	if e != nil {
		return false
	}
	release()
	return true
}
func (s Store) List(ctx context.Context, profile string) ([]Record, *protocol.Error) {
	items := []Record{}
	if profile != "" && !project.ValidProfile(profile) {
		return nil, failure("invalid_argument")
	}
	entries, e := os.ReadDir(s.root())
	if errors.Is(e, os.ErrNotExist) {
		return items, nil
	}
	if e != nil {
		return nil, storageError()
	}
	for _, entry := range entries {
		if !validID(entry.Name()) {
			continue
		}
		r, e := s.Status(ctx, entry.Name())
		if e != nil {
			if e.Code == "process_not_found" {
				continue
			}
			return nil, e
		}
		if profile == "" || r.Profile == profile {
			items = append(items, r)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}
func (s Store) Active(ctx context.Context, instance string) *protocol.Error {
	items, e := s.List(ctx, "")
	if e != nil {
		return e
	}
	for _, r := range items {
		if r.Instance == instance && ((r.EndedAt == nil && r.State != "interrupted") || !s.leaseFree(ctx, r.ID)) {
			return failure("process_active")
		}
	}
	return nil
}
func (s Store) Lock(ctx context.Context) (func(), error) {
	return tasks.Lock(ctx, filepath.Join(s.root(), "operations.lock"))
}
func (s Store) Apply(ctx context.Context, q Request) (Result, *protocol.Error) {
	var out Result
	if !validID(q.RequestID) {
		return out, failure("invalid_argument")
	}
	if q.Action == "start" && q.ID != "" || q.Action != "start" && (q.Command != "" || q.Directory != "") {
		return out, failure("invalid_argument")
	}
	unlock, e := tasks.Lock(ctx, filepath.Join(s.root(), "operations.lock"))
	if e != nil {
		return out, storageError()
	}
	defer unlock()
	b, _ := json.Marshal(q)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(b))
	receiptPath := filepath.Join(s.root(), "receipts", q.RequestID+".json")
	var old receipt
	if e = tasks.ReadPrivate(receiptPath, &old); e == nil {
		if old.Fingerprint != fingerprint {
			return out, failure("request_conflict")
		}
		if old.Done {
			old.Result.Replayed = true
			return old.Result, nil
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return out, storageError()
	}
	if writePrivate(receiptPath, receipt{Fingerprint: fingerprint}) != nil {
		return out, storageError()
	}
	var err *protocol.Error
	switch q.Action {
	case "start":
		out, err = s.start(ctx, q, "")
	case "stop", "restart":
		var r Record
		r, err = s.read(q.ID)
		if err != nil {
			break
		}
		if q.Action == "stop" && (q.Env != nil || q.Capture != nil) {
			err = failure("invalid_argument")
			break
		}
		out, err = s.stop(ctx, r)
		if err != nil {
			break
		}
		if q.Action == "restart" {
			q.Directory = r.Directory
			q.Command = r.Command
			if q.Env == nil {
				q.Env = r.EnvOverride
			}
			if q.Capture == nil {
				q.Capture = &r.Capture
			}
			out, err = s.start(ctx, q, r.ID)
		}
	default:
		err = failure("invalid_argument")
	}
	if err != nil {
		return out, err
	}
	if writePrivate(receiptPath, receipt{Fingerprint: fingerprint, Result: out, Done: true}) != nil {
		return out, storageError()
	}
	return out, nil
}
func (s Store) start(ctx context.Context, q Request, previous string) (Result, *protocol.Error) {
	var out Result
	// Request identity also names the execution, so retry after a lost receipt
	// observes the same run instead of launching another command.
	if r, e := s.read(q.RequestID); e == nil {
		out.Item = r
		return out, nil
	} else if e.Code != "process_not_found" {
		return out, e
	}
	p, e := project.Resolve(q.Directory, "")
	if e != nil {
		return out, e
	}
	c, ok := p.Commands[q.Command]
	if !ok {
		return out, failure("command_not_found")
	}
	env := c.Env
	if q.Env != nil {
		env = *q.Env
		if env != "" && !project.ValidProfile(env) {
			return out, failure("invalid_argument")
		}
	}
	ps := ports.Store{Directory: filepath.Join(s.Data, "ports")}
	var instance ports.Instance
	e = ps.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		i := st.Find(p.Profile, "", p.Root)
		if i != nil {
			instance = *i
			return false, nil
		}
		i, e := st.Register(p.Profile, p.Root)
		if e != nil {
			return false, e
		}
		instance = *i
		return true, nil
	})
	if e != nil {
		return out, e
	}
	list, e := s.List(ctx, p.Profile)
	if e != nil {
		return out, e
	}
	capture := q.Capture != nil && *q.Capture
	for _, r := range list {
		if r.Instance == instance.ID && r.Command == q.Command && r.EndedAt == nil && r.State != "interrupted" {
			if r.State == "unknown" {
				return out, failure("process_unavailable")
			}
			if r.Env != env || r.Capture != capture {
				return out, failure("process_conflict")
			}
			return Result{Item: r}, nil
		}
	}
	r := Record{ID: q.RequestID, Profile: p.Profile, Instance: instance.ID, Directory: p.Root, Command: q.Command, Env: env, EnvOverride: q.Env, Capture: capture, CreatedAt: time.Now().UTC(), State: "starting", Previous: previous}
	if writePrivate(s.path(r.ID, "record.json"), r) != nil {
		return out, storageError()
	}
	exe, err := os.Executable()
	if err != nil {
		return out, storageError()
	}
	cmd := exec.Command(exe, "__process-serve", s.Data, r.ID)
	cmd.Dir = p.Root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		now := time.Now().UTC()
		r.EndedAt = &now
		r.State = "failed"
		r.Reason = "supervisor_start_failed"
		_ = writePrivate(s.path(r.ID, "record.json"), r)
		return Result{Item: r, Changed: true}, nil
	}
	go func() { _ = cmd.Wait() }()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return out, failure("process_pending")
		case <-deadline.C:
			return out, failure("process_pending")
		case <-tick.C:
			current, e := s.read(r.ID)
			if e != nil {
				return out, e
			}
			if current.StartedAt != nil || current.EndedAt != nil {
				return Result{Item: current, Changed: true}, nil
			}
		}
	}
}
func (s Store) stop(ctx context.Context, r Record) (Result, *protocol.Error) {
	if r.EndedAt != nil {
		return Result{Item: r}, nil
	}
	if _, e := s.rpc(ctx, r.ID, "stop"); e != nil {
		if s.leaseFree(ctx, r.ID) {
			now := time.Now().UTC()
			r.EndedAt = &now
			r.State = "interrupted"
			r.Reason = "supervisor_lost"
			if writePrivate(s.path(r.ID, "record.json"), r) != nil {
				return Result{}, storageError()
			}
			return Result{Item: r, Changed: true}, nil
		}
		return Result{}, failure("process_unavailable")
	}
	wait, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-wait.Done():
			return Result{}, failure("process_pending")
		case <-tick.C:
			current, e := s.read(r.ID)
			if e != nil {
				return Result{}, e
			}
			if current.EndedAt != nil && s.leaseFree(ctx, r.ID) {
				return Result{Item: current, Changed: true}, nil
			}
		}
	}
}
func (s Store) Logs(ctx context.Context, id string) (string, *protocol.Error) {
	r, e := s.Status(ctx, id)
	if e != nil {
		return "", e
	}
	if !r.Capture {
		return "", failure("logs_disabled")
	}
	if r.EndedAt != nil && time.Since(*r.EndedAt) > 7*24*time.Hour {
		return "", failure("logs_expired")
	}
	b, err := maintenance.Read(s.path(id, "output.log"), 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", storageError()
	}
	return string(bytes.ToValidUTF8(b, []byte("�"))), nil
}
