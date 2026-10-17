package expose

import (
	"context"
	"io"
	"net"
	"net/url"

	"os/exec"
	"path/filepath"
	"regexp"

	"sync"

	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Cloudflared struct {
	Data                   string
	Executable, Supervisor string
	Timeout                time.Duration
}

var quickTunnelURL = regexp.MustCompile(`https://[a-z0-9]+(?:-[a-z0-9]+)*\.trycloudflare\.com(?:[^a-zA-Z0-9.-]|$)`)

type cloudflaredSession struct {
	command    *exec.Cmd
	lifeline   io.WriteCloser
	done       chan struct{}
	url, owner string
	once       sync.Once
}

func (p Cloudflared) statePath(r Record) string {
	return filepath.Join(p.Data, "exposure-providers", r.ID+"-"+r.Generation+".json")
}
func (p Cloudflared) RecoverStop(ctx context.Context, r Record) *protocol.Error {
	if p.Data == "" || r.ProviderOwner != r.Generation {
		return Failure("provider_ownership_invalid")
	}
	var state struct{ State string }
	if tasks.ReadPrivate(p.statePath(r), &state) != nil || state.State != "stopped" {
		return Failure("provider_recovery_required")
	}
	return nil
}
func (s *cloudflaredSession) URL() string   { return s.url }
func (s *cloudflaredSession) Owner() string { return s.owner }
func (s *cloudflaredSession) Alive(context.Context) bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
func (s *cloudflaredSession) Stop(ctx context.Context) *protocol.Error {
	s.once.Do(func() { _ = s.lifeline.Close() })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return Failure("provider_stop_pending")
	case <-time.After(5 * time.Second):
		_ = s.command.Process.Kill()
		return Failure("provider_stop_pending")
	}
}

func ValidOrigin(origin string) bool {
	u, e := url.Parse(origin)
	if e != nil {
		return false
	}
	host, port, e := net.SplitHostPort(u.Host)
	return e == nil && host == "127.0.0.1" && port != "" && u.Scheme == "http" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
