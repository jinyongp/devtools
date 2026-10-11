package netsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type replacement struct {
	Previous Intent   `json:"previous"`
	Next     Identity `json:"next"`
}

func (m Manager) replacementPath(destination string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(destination)))
	return filepath.Join(m.Data, "netsetup", "replacement-"+hex.EncodeToString(digest[:])+".json")
}

// PrepareReplacement runs on the verified release candidate while the installer
// owns its lock. Failure leaves the installed binary and its intent intact.
func (m Manager) PrepareReplacement(ctx context.Context, destination string) *protocol.Error {
	if m.Isolated {
		return failure("isolated_operation_not_allowed")
	}
	intent, e := m.ReadIntent()
	if e != nil {
		return e
	}
	if !intent.Enabled {
		return nil
	}
	old, previous, e := OpenIdentity(destination)
	if old != nil {
		old.Close()
	}
	if e != nil {
		return e
	}
	if intent.Identity.Path != previous.Path {
		return failure("setup_intent_conflict")
	}
	f, next, e := OpenIdentity(m.executable())
	if e != nil {
		return e
	}
	defer f.Close()
	pending := replacement{Previous: intent, Next: next}
	pending.Next.Path = previous.Path
	if tasks.WritePrivate(m.replacementPath(destination), pending) != nil {
		return failure("io_error")
	}
	if m.goos() == "linux" {
		run := m.Run
		if run == nil {
			run = privilege.Run
		}
		if e := run(ctx, "netsetup", Request{Identity: next, Enable: true}, m.Terminal); e != nil {
			return e
		}
	}
	return nil
}
func (m Manager) CommitReplacement(destination string, rollback bool) *protocol.Error {
	intent, e := m.ReadIntent()
	if e != nil {
		return e
	}
	if !intent.Enabled {
		return nil
	}
	var pending replacement
	if tasks.ReadPrivate(m.replacementPath(destination), &pending) != nil {
		return failure("recovery_required")
	}
	f, identity, e := OpenIdentity(destination)
	if f != nil {
		f.Close()
	}
	if e != nil {
		return e
	}
	if rollback {
		if identity != pending.Previous.Identity {
			return failure("request_conflict")
		}
		intent = pending.Previous
	} else {
		if identity != pending.Next {
			return failure("request_conflict")
		}
		intent = Intent{Identity: identity, Enabled: true}
	}
	return m.writeIntent(intent)
}
