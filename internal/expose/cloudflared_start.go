package expose

import (
	"bufio"
	"context"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func (p Cloudflared) Start(ctx context.Context, r Record, origin string) (ProviderSession, *protocol.Error) {
	executable := p.Executable
	var e error
	if executable == "" {
		executable, e = exec.LookPath("cloudflared")
		if e != nil {
			return nil, Failure("cloudflared_not_installed")
		}
	}
	if !cloudflaredExecutable(executable) {
		return nil, Failure("cloudflared_not_installed")
	}
	executable, e = filepath.Abs(executable)
	if e != nil {
		return nil, Failure("cloudflared_not_installed")
	}
	supervisor := p.Supervisor
	if supervisor == "" {
		supervisor, e = os.Executable()
		if e != nil {
			return nil, Failure("io_error")
		}
	}
	read, write, e := os.Pipe()
	if e != nil {
		return nil, Failure("io_error")
	}
	args := []string{"__exposure-provider", executable, origin}
	if p.Data != "" {
		r.ProviderOwner = r.Generation
		if e := (Store{Data: p.Data}).Write(r); e != nil {
			read.Close()
			write.Close()
			return nil, e
		}
		args = append(args, p.statePath(r))
	}
	cmd := exec.Command(supervisor, args...)
	cmd.Stdin = read
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		read.Close()
		write.Close()
		return nil, Failure("io_error")
	}
	stderr, e := cmd.StderrPipe()
	if e != nil {
		read.Close()
		write.Close()
		stdout.Close()
		return nil, Failure("io_error")
	}
	if e := startCloudflaredSupervisor(cmd); e != nil {
		read.Close()
		write.Close()
		stdout.Close()
		stderr.Close()
		return nil, Failure("cloudflared_start_failed")
	}
	read.Close()
	s := &cloudflaredSession{command: cmd, lifeline: write, done: make(chan struct{}), owner: r.Generation}
	urls := make(chan string, 2)
	scanDone := make(chan struct{}, 2)
	scan := func(reader io.Reader) {
		defer func() { scanDone <- struct{}{} }()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 65536)
		for scanner.Scan() {
			match := quickTunnelURL.FindString(scanner.Text())
			if match != "" {
				match = strings.TrimRight(match, " /|\\\"'<>)]},;\t\r\n")
				u, e := url.Parse(match)
				if e == nil && u.Scheme == "https" && strings.HasSuffix(u.Hostname(), ".trycloudflare.com") && u.Port() == "" && u.User == nil {
					select {
					case urls <- u.Scheme + "://" + u.Host:
					default:
					}
				}
			}
		}
	}
	go scan(stdout)
	go scan(stderr)
	go func() { <-scanDone; <-scanDone; _ = cmd.Wait(); close(s.done) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Stop(context.Background())
		case <-s.done:
		}
	}()
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case s.url = <-urls:
		select {
		case <-s.done:
			_ = s.Stop(context.Background())
			return nil, Failure("cloudflared_exited")
		case <-ctx.Done():
			_ = s.Stop(context.Background())
			return nil, Failure("canceled")
		case <-time.After(100 * time.Millisecond):
		}
		return s, nil
	case <-s.done:
		_ = s.Stop(context.Background())
		// Scanners finish before done closes. Prefer their observed URL when
		// process exit and URL delivery are both ready for this select.
		select {
		case <-urls:
			return nil, Failure("cloudflared_exited")
		default:
			return nil, Failure("cloudflared_exited_without_url")
		}
	case <-ctx.Done():
		_ = s.Stop(context.Background())
		return nil, Failure("canceled")
	case <-timer.C:
		_ = s.Stop(context.Background())
		return nil, Failure("cloudflared_url_timeout")
	}
}
