package services

import (
	"context"
	"io"
	"time"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type readyKey struct{}

// WithReadyProbe passes the selected command's configuration to its supervisor.
// The prepared environment is retained in memory by the runner, not persisted.
func WithReadyProbe(ctx context.Context, probe *project.ReadyProbe) context.Context {
	return context.WithValue(ctx, readyKey{}, probe)
}

type Readiness struct {
	Ready     bool      `json:"ready"`
	CheckedAt time.Time `json:"checked_at"`
	Reason    string    `json:"reason"`
	ExitCode  *int      `json:"exit_code"`
}

func runProbe(ctx context.Context, p project.ReadyProbe, dir string, env []string) Readiness {
	ctx, cancel := context.WithTimeout(ctx, p.Duration())
	defer cancel()
	code, err := process.Execute(ctx, p.Exec, dir, env, nil, io.Discard, io.Discard)
	r := Readiness{CheckedAt: time.Now().UTC(), Reason: "probe_failed"}
	if ctx.Err() != nil {
		r.Reason = "probe_timeout"
		return r
	}
	if err != nil {
		r.Reason = "probe_execution_failed"
		return r
	}
	r.ExitCode = &code
	if code == 0 {
		r.Ready = true
		r.Reason = "probe_passed"
	}
	return r
}

func (s Store) Check(ctx context.Context, id string) (Record, *protocol.Error) {
	r, e := s.read(id)
	if e != nil {
		return r, e
	}
	if r.EndedAt != nil {
		return r, failure("process_not_running")
	}
	live, err := s.rpc(ctx, id, "check")
	if err != nil {
		if ctx.Err() != nil {
			return r, protocol.NewError("canceled", "Readiness check canceled.", 130, nil)
		}
		return r, failure("readiness_unavailable")
	}
	if live.EndedAt != nil || live.State != "running" {
		return live, failure("process_not_running")
	}
	if !live.ReadyConfigured {
		return live, failure("readiness_not_configured")
	}
	if live.Readiness == nil {
		return live, failure("readiness_unavailable")
	}
	return live, nil
}

func (s Store) Wait(ctx context.Context, id string, timeout time.Duration) (Record, *protocol.Error) {
	if timeout <= 0 || timeout > 10*time.Minute {
		return Record{}, failure("invalid_argument")
	}
	wait, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last Record
	for {
		r, err := s.Check(wait, id)
		last = r
		if ctx.Err() != nil {
			return last, protocol.NewError("canceled", "Readiness wait canceled.", 130, nil)
		}
		if wait.Err() != nil {
			return last, protocol.NewError("readiness_timeout", "Process readiness wait timed out.", 3, map[string]any{"id": id})
		}
		if err != nil {
			return last, err
		}
		if r.Readiness.Ready {
			return r, nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-wait.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
