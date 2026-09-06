package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Execute func(context.Context, Record, process.Runner, io.Writer) *protocol.Error

// Serve owns a session/process group. Only this live group leader signals its
// own group; clients never signal a PID read from disk.
func (s Store) Serve(ctx context.Context, id string, execute Execute) error {
	if !validID(id) {
		return errors.New("invalid ID")
	}
	pid := os.Getpid()
	group, e := syscall.Getpgid(pid)
	if e != nil || group != pid {
		return errors.New("dedicated session required")
	}
	release, e := tasks.Lock(ctx, s.path(id, "lease.lock"))
	if e != nil {
		return e
	}
	defer release()
	r, err := s.read(id)
	if err != nil || r.EndedAt != nil {
		return errors.New("invalid execution")
	}
	// Keeping the supervisor alive reserves the process group identity through
	// terminal metadata publication and descendant cleanup.
	defer syscall.Kill(-pid, syscall.SIGKILL)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var probe func(context.Context) Readiness
	probeGate := make(chan struct{}, 1)
	probeGate <- struct{}{}
	defer func() { cancel(); <-probeGate }()
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return e
	}
	b := make([]byte, 32)
	if _, e = rand.Read(b); e != nil {
		listener.Close()
		return e
	}
	c := control{ID: id, Address: "http://" + listener.Addr().String(), Token: hex.EncodeToString(b)}
	server := &http.Server{ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 35 * time.Second, MaxHeaderBytes: 8192}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if q.Host != strings.TrimPrefix(c.Address, "http://") || q.Header.Get("Origin") != "" || q.Method != "POST" || q.Header.Get("Authorization") != "Bearer "+c.Token {
			http.Error(w, "Forbidden", 403)
			return
		}
		if q.URL.Path != "/status" && q.URL.Path != "/stop" && q.URL.Path != "/check" {
			http.NotFound(w, q)
			return
		}
		mu.Lock()
		snapshot := r
		check := probe
		mu.Unlock()
		if q.URL.Path == "/check" && snapshot.State == "running" && check != nil {
			select {
			case <-q.Context().Done():
				return
			case <-ctx.Done():
				return
			case <-probeGate:
			}
			defer func() { probeGate <- struct{}{} }()
			if ctx.Err() != nil {
				return
			}
			query, cancelQuery := context.WithCancel(q.Context())
			stop := context.AfterFunc(ctx, cancelQuery)
			result := check(query)
			stop()
			cancelQuery()
			mu.Lock()
			snapshot = r
			mu.Unlock()
			if ctx.Err() != nil || snapshot.EndedAt != nil {
				result.Ready = false
				result.Reason = "process_stopped"
			}
			snapshot.Readiness = &result
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
		if q.URL.Path == "/stop" {
			cancel()
		}
	})
	if writePrivate(s.path(id, "control.json"), c) != nil {
		listener.Close()
		return errors.New("control storage failed")
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	var output io.Writer = io.Discard
	if r.Capture {
		output = &boundedLog{path: s.path(id, "output.log")}
	}
	childExit := 0
	ran := false
	runner := func(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
		path, err := process.LookPath(args[0], dir, env)
		if err != nil {
			return 0, err
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Dir = dir
		cmd.Env = env
		cmd.Stdin = in
		cmd.Stdout = out
		cmd.Stderr = diagnostic
		// Inherit the supervisor group, including conventional child processes.
		cmd.WaitDelay = 250 * time.Millisecond
		if ctx.Err() != nil {
			return 0, failure("canceled")
		}
		if cmd.Start() != nil {
			return 0, failure("execution_failed")
		}
		now := time.Now().UTC()
		mu.Lock()
		if config, ok := ctx.Value(readyKey{}).(*project.ReadyProbe); ok && config != nil {
			copy := *config
			copy.Exec = append([]string{}, config.Exec...)
			preparedEnv := append([]string{}, env...)
			probe = func(query context.Context) Readiness { return runProbe(query, copy, dir, preparedEnv) }
			r.ReadyConfigured = true
		}
		r.StartedAt = &now
		r.State = "running"
		writeErr := writePrivate(s.path(id, "record.json"), r)
		mu.Unlock()
		if writeErr != nil {
			cancel()
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		var waitErr error
		select {
		case waitErr = <-done:
		case <-ctx.Done():
			_ = syscall.Kill(-pid, syscall.SIGTERM)
			select {
			case waitErr = <-done:
			case <-time.After(3 * time.Second):
				childExit = 137
				ran = true
				return childExit, nil
			}
		}
		ran = true
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			childExit = exit.ExitCode()
			if st, ok := exit.Sys().(syscall.WaitStatus); ok && st.Signaled() {
				childExit = 128 + int(st.Signal())
			}
		} else if waitErr != nil && !errors.Is(waitErr, exec.ErrWaitDelay) {
			childExit = 1
		}
		return childExit, nil
	}
	err = execute(ctx, r, runner, output)
	mu.Lock()
	now := time.Now().UTC()
	r.EndedAt = &now
	r.State = "exited"
	r.ExitCode = &childExit
	if ctx.Err() != nil {
		r.State = "stopped"
		r.Reason = "stop_requested"
	} else if err != nil {
		r.State = "failed"
		r.Reason = err.Code
		r.ExitCode = nil
	} else if ran && childExit != 0 {
		r.Reason = "nonzero_exit"
	}
	e = writePrivate(s.path(id, "record.json"), r)
	mu.Unlock()
	return e
}

// Each write retains at most the last MiB. stdout and stderr share a lock and
// private atomic replacement, so readers see a complete bounded snapshot.
type boundedLog struct {
	mu   sync.Mutex
	path string
	data []byte
}

func (l *boundedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	if n >= 1<<20 {
		l.data = append(l.data[:0], p[n-(1<<20):]...)
	} else {
		l.data = append(l.data, p...)
		if len(l.data) > 1<<20 {
			l.data = append([]byte{}, l.data[len(l.data)-(1<<20):]...)
		}
	}
	if e := maintenance.Write(l.path, l.data); e != nil {
		return 0, e
	}
	return n, nil
}
