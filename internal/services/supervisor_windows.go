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

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
)

// The detached supervisor owns a console; each application has a separate Job.
func (s Store) serve(ctx context.Context, id string, inherited *os.File, execute Execute) error {
	if !validID(id) {
		return errors.New("invalid ID")
	}
	if err := process.RequireDedicatedConsole(); err != nil {
		return err
	}
	_, hasMarker, markerErr := s.readLaunchMarker(id)
	if markerErr != nil {
		return errors.New("invalid launch marker")
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
		if err != nil {
			return err
		}
		if !acquired {
			return errors.New("lease unavailable")
		}
	}
	defer func() { _ = lease.unlockClose() }()
	r, err := s.read(id)
	if err != nil || r.EndedAt != nil {
		return errors.New("invalid execution")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	var probe func(context.Context) Readiness
	probeGate := make(chan struct{}, 1)
	probeGate <- struct{}{}
	defer func() { cancel(); <-probeGate }()
	listener, e := ports.ListenTCP(ctx, "tcp4", "127.0.0.1:0")
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
		if err := process.ValidateManaged(args, dir, env); err != nil {
			return 0, err
		}
		code, runErr := process.ExecuteOwned(ctx, args, dir, env, in, out, diagnostic, func() *protocol.Error {
			now := time.Now().UTC()
			mu.Lock()
			defer mu.Unlock()
			next := r
			if selection, ok := ctx.Value(environmentSelectionKey{}).(environmentSelection); ok {
				next.Env = selection.Env
				next.Inject = &selection.Inject
			}
			config := process.ReadyProbe(ctx)
			next.ReadyConfigured = config != nil
			next.StartedAt = &now
			next.State = "running"
			if next.InputSnapshotID != "" {
				next.LaunchPhase = "spawned"
			}
			if writePrivate(s.path(id, "record.json"), next) != nil {
				return storageError()
			}
			r = next
			if config != nil {
				preparedEnv := append([]string{}, env...)
				probe = func(query context.Context) Readiness { return runProbe(query, *config, dir, preparedEnv) }
			}
			return nil
		})
		if runErr == nil {
			ran = true
			childExit = code
		}
		return code, runErr
	}

	err = execute(ctx, r, runner, output)
	mu.Lock()
	s.finishInputPhase(&r)
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
