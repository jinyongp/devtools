// Package netsetup manages explicit low-port permission intent for the installed
// executable. The daemon always attempts the actual bind independently of intent.
package netsetup

import (
	"context"

	"errors"

	"os"
	"path/filepath"
	"runtime"

	"github.com/jinyongp/devtools/internal/netops"

	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Identity struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Digest string `json:"digest"`
}
type Request struct {
	Identity Identity `json:"identity"`
	Enable   bool     `json:"enable"`
}
type Intent struct {
	Identity  Identity `json:"identity"`
	Enabled   bool     `json:"enabled"`
	Preparing bool     `json:"preparing"`
}
type Manager struct {
	Data, Executable   string
	Isolated, Terminal bool
	Run                privilege.Runner
	GOOS               string
}

func failure(code string) *protocol.Error {
	return protocol.NewError(code, "Cannot prepare low-port access safely. Linux binaries need a filesystem supporting security capabilities; install on the Linux filesystem instead of a Windows mount, or use high ports.", 3, nil)
}

func (m Manager) executable() string {
	if m.Executable != "" {
		return m.Executable
	}
	path, _ := os.Executable()
	return path
}
func (m Manager) goos() string {
	if m.GOOS != "" {
		return m.GOOS
	}
	return runtime.GOOS
}
func (m Manager) intentPath() string { return filepath.Join(m.Data, "netsetup", "intent.json") }
func (m Manager) writeIntent(intent Intent) *protocol.Error {
	if tasks.PrivateDir(filepath.Dir(m.intentPath())) != nil || tasks.WritePrivate(m.intentPath(), intent) != nil {
		return failure("io_error")
	}
	return nil
}
func (m Manager) ReadIntent() (Intent, *protocol.Error) {
	var intent Intent
	e := tasks.ReadPrivate(m.intentPath(), &intent)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return intent, failure("invalid_storage")
	}
	return intent, nil
}
func (m Manager) Check() (map[string]any, *protocol.Error) {
	f, identity, e := OpenIdentity(m.executable())
	if e != nil {
		return nil, e
	}
	defer f.Close()
	intent, e := m.ReadIntent()
	if e != nil {
		return nil, e
	}
	actual := false
	mode := "native"
	if m.goos() == "linux" {
		mode = "capability"
		actual, e = capability(f)
		if e != nil {
			return nil, e
		}
	}
	return map[string]any{"mode": mode, "enabled": actual, "intent_enabled": intent.Enabled, "executable_changed": intent.Identity.Path != "" && intent.Identity != identity, "recovery_required": intent.Enabled && (intent.Preparing || intent.Identity != identity || m.goos() == "linux" && !actual), "bind_check": "performed_when_listener_starts"}, nil
}
func (m Manager) Apply(ctx context.Context, id string, enable bool) (any, *protocol.Error) {
	if m.Isolated {
		return nil, netops.Failure("isolated_operation_not_allowed", "Shared executable permissions cannot be changed in isolated mode.")
	}
	path, e := filepath.EvalSymlinks(m.executable())
	if e != nil {
		return nil, failure("executable_unavailable")
	}
	return (netops.Store{Data: m.Data}).Apply(ctx, id, struct {
		Action, Path string
		Enable       bool
	}{"setup", path, enable}, []string{"installed-binary/" + path}, func(j *netops.Journal) (any, *protocol.Error) {
		lock := filepath.Join(filepath.Dir(path), ".devtools-install.lock")
		if e := os.Mkdir(lock, 0700); e != nil {
			return nil, failure("installation_busy")
		}
		defer os.Remove(lock)
		f, identity, e := OpenIdentity(path)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		var expected Identity
		present, e := j.Get("identity", &expected)
		if e != nil {
			return nil, e
		}
		if present && identity != expected {
			return nil, failure("request_conflict")
		}
		if !present {
			expected = identity
			if e := j.Set("identity", expected); e != nil {
				return nil, e
			}
		}
		intent := Intent{Identity: expected, Enabled: enable, Preparing: true}
		if e := m.writeIntent(intent); e != nil {
			return nil, e
		}
		if m.goos() == "linux" {
			run := m.Run
			if run == nil {
				run = privilege.Run
			}
			if e := run(ctx, "netsetup", Request{expected, enable}, m.Terminal); e != nil {
				return nil, e
			}
		}
		intent.Preparing = false
		if e := m.writeIntent(intent); e != nil {
			return nil, e
		}
		return map[string]any{"changed": true, "intent_enabled": enable, "mode": m.goos()}, nil
	})
}

// SystemApply accepts only the helper's own executable and operates through the
// verified open descriptor. Replacing a path cannot redirect the capability write.
func SystemApply(ctx context.Context, q Request, executable string) *protocol.Error {
	if ctx.Err() != nil {
		return failure("canceled")
	}
	f, identity, e := OpenIdentity(executable)
	if e != nil {
		return e
	}
	defer f.Close()
	if identity != q.Identity {
		return failure("request_conflict")
	}
	if e := setCapability(f, q.Enable); e != nil {
		return e
	}
	current, after, e := OpenIdentity(executable)
	if current != nil {
		current.Close()
	}
	if e != nil || after != identity {
		_ = setCapability(f, false)
		return failure("request_conflict")
	}
	return nil
}
