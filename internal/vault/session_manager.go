package vault

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/process"
)

// SessionManager starts this executable's private daemon on demand. No project
// configuration, task context, inherited injected values, or shell is involved.
type SessionManager struct {
	Directory  string
	Executable string
}

func (m SessionManager) Client(ctx context.Context) (*SessionClient, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := (KeyFileStore{Directory: m.Directory}).Read(); err != nil {
		return nil, err
	}
	r, err := platformfs.OpenRoot(m.Directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	r.Close()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c, err := OpenSessionClient(m.Directory); err == nil {
			if _, err = c.Status(ctx); err == nil {
				return c, nil
			}
		}
		release, acquired, err := maintenance.TryLockExclusive(ctx, filepath.Join(m.Directory, "session-start.lock"))
		if err != nil {
			return nil, ErrRecovery
		}
		if !acquired {
			if err = waitSession(ctx); err != nil {
				return nil, err
			}
			continue
		}
		c, err := m.start(ctx)
		release()
		return c, err
	}
}
func waitSession(ctx context.Context) error {
	t := time.NewTimer(20 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (m SessionManager) start(ctx context.Context) (*SessionClient, error) {
	if c, err := OpenSessionClient(m.Directory); err == nil {
		if _, err = c.Status(ctx); err == nil {
			return c, nil
		}
	}
	if e, err := ReadSessionEndpoint(m.Directory); err == nil && e.Protocol != SessionProtocol {
		// Only the stable control handshake can retire a mismatched daemon;
		// never send it a versioned key operation or kill a PID from metadata.
		old := &SessionClient{Directory: m.Directory, Endpoint: *e}
		if err = old.Retire(ctx); err != nil && !errors.Is(err, ErrUnavailable) {
			return nil, ErrSessionProtocol
		}
	}
	// A retirement acknowledgement precedes complete worker shutdown. Wait for
	// the real OS lease before starting a successor with the same endpoint.
	for {
		release, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(m.Directory, "session-daemon.lock"))
		if err != nil {
			return nil, ErrRecovery
		}
		if free {
			release()
			break
		}
		if err = waitSession(ctx); err != nil {
			return nil, err
		}
	}
	exe := m.Executable
	if exe == "" {
		var err error
		exe, err = os.Executable()
		if err != nil {
			return nil, ErrUnavailable
		}
	}
	if !filepath.IsAbs(exe) {
		return nil, ErrInvalid
	}
	cmd := exec.Command(exe, "__vault-session", m.Directory)
	cmd.Env = sessionDaemonEnvironment(os.Environ())
	// os/exec connects nil stdio to the null device. The daemon never inherits
	// terminal input or a parent pipe containing secrets.
	if err := process.StartDetached(cmd); err != nil {
		return nil, ErrUnavailable
	}
	cmd.Process.Release()
	for {
		if c, err := OpenSessionClient(m.Directory); err == nil {
			if _, err = c.Status(ctx); err == nil {
				return c, nil
			}
		}
		if err := waitSession(ctx); err != nil {
			return nil, err
		}
	}
}
func sessionDaemonEnvironment(parent []string) []string {
	allowed := map[string]bool{"HOME": true, "USER": true, "LOGNAME": true, "PATH": true, "TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LC_ALL": true, "DBUS_SESSION_BUS_ADDRESS": true, "XDG_RUNTIME_DIR": true, "XDG_DATA_HOME": true, "XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true, "LOCALAPPDATA": true, "APPDATA": true, "USERPROFILE": true, "SYSTEMROOT": true, "WINDIR": true, "SYSTEMDRIVE": true, "DEVTOOLS_ISOLATED_ROOT": true}
	var result []string
	for _, item := range parent {
		name, _, ok := strings.Cut(item, "=")
		if ok && allowed[strings.ToUpper(name)] {
			result = append(result, item)
		}
	}
	return result
}
