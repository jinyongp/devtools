package vault

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

// LaunchTicket binds prepared input to the key delivery that authenticated it.
// It is public metadata, not a bearer credential: admission also checks OS peers,
// the current live session, its lock epoch, and its unlocked key cache.
type LaunchTicket struct {
	VaultID   string    `json:"vault_id"`
	SessionID string    `json:"session_id"`
	Epoch     uint64    `json:"epoch"`
	ExpiresAt time.Time `json:"expires_at"`
}

type launchRecord struct {
	Format  int          `json:"format"`
	ID      string       `json:"id"`
	Ticket  LaunchTicket `json:"ticket"`
	Outcome string       `json:"outcome"`
}

// LaunchAdmission is owned by the launcher process itself, never by a client
// that can die while the launcher continues. Finish at the actual spawn outcome,
// before waiting for application output or readiness. A crash releases the OS
// gate but leaves an unknown journal entry; lock must then report pending.
type LaunchAdmission struct {
	mu        sync.Mutex
	directory string
	record    launchRecord
	release   func()
}

func launchGate(directory string) string { return filepath.Join(directory, "launch.lock") }
func launchName(id string) string        { return filepath.Join("launches", id+".json") }
func validLaunchTicket(t LaunchTicket) bool {
	return validID(t.VaultID) && validID(t.SessionID) && !t.ExpiresAt.IsZero()
}

func (s *KeySession) authorizeLaunch(ctx context.Context, ticket LaunchTicket, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validLaunchTicket(ticket) || ticket.SessionID != sessionID || ticket.Epoch != s.epoch {
		return ErrLocked
	}
	if !s.now().Before(ticket.ExpiresAt) {
		return ErrLocked
	}
	k, err := s.read()
	if err != nil {
		return err
	}
	if k.VaultID != ticket.VaultID {
		return ErrIntegrity
	}
	if s.key == nil || s.status.State != Unlocked {
		return ErrLocked
	}
	s.touch()
	return nil
}

// BeginLaunch must be called after the launcher has received/validated all input
// and before stopping an old execution or spawning a child. No key is returned.
func (c *SessionClient) BeginLaunch(ctx context.Context, id string, ticket LaunchTicket) (*LaunchAdmission, error) {
	if !validID(id) || !validLaunchTicket(ticket) || ticket.VaultID != c.Endpoint.VaultID || ticket.SessionID != c.Endpoint.SessionID {
		return nil, ErrInvalid
	}
	release, err := maintenance.LockShared(ctx, launchGate(c.Directory))
	if err != nil {
		return nil, ErrLockPending
	}
	admitted := false
	defer func() {
		if !admitted {
			release()
		}
	}()
	if _, err = c.requestWithTicket(ctx, "admit", nil, false, &ticket); err != nil {
		return nil, err
	}
	r, err := platformfs.OpenRoot(c.Directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	defer r.Close()
	if err = r.MkdirAll("launches"); err != nil {
		return nil, ErrRecovery
	}
	record := launchRecord{Format: StorageVersion, ID: id, Ticket: ticket, Outcome: "unknown"}
	b, _ := json.Marshal(record)
	if err = r.WriteFile(ctx, launchName(id), b, false); err != nil {
		// An uncertain previous admission is never retried as a new spawn.
		return nil, ErrRecovery
	}
	admitted = true
	return &LaunchAdmission{directory: c.Directory, record: record, release: release}, nil
}

// Finish records a known outcome or retains unknown. It always drops the live
// gate, even when publication fails; the durable unknown then blocks lock ack.
func (a *LaunchAdmission) Finish(ctx context.Context, outcome string) error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.release == nil {
		return ErrInvalid
	}
	defer func() { a.release(); a.release = nil }()
	if outcome != "spawned" && outcome != "canceled" && outcome != "failed" {
		return ErrLockPending
	}
	r, err := platformfs.OpenRoot(a.directory, false)
	if err != nil {
		return ErrRecovery
	}
	defer r.Close()
	a.record.Outcome = outcome
	b, _ := json.Marshal(a.record)
	if r.WriteFile(ctx, launchName(a.record.ID), b, true) != nil {
		return ErrRecovery
	}
	return nil
}

// DrainLaunches runs after durable manual lock intent, outside all session state
// mutexes. The OS gate survives daemon replacement and is owned by actual live
// launchers. Unknown journals are deliberately not inferred from a PID.
func DrainLaunches(ctx context.Context, directory string) error {
	release, err := maintenance.LockExclusive(ctx, launchGate(directory))
	if err != nil {
		return ErrLockPending
	}
	defer release()
	root, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return ErrRecovery
	}
	defer root.Close()
	dir := filepath.Join(directory, "launches")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ErrRecovery
	}
	guard, err := platformfs.OpenRoot(dir, false)
	if err != nil {
		return ErrRecovery
	}
	defer guard.Close()
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".write-") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || entry.Name() != id+".json" || !validID(id) {
			return ErrRecovery
		}
		b, e := root.ReadFile(launchName(id), MaxKeySlotBytes)
		var record launchRecord
		if e != nil || StrictDecode(b, &record) != nil || record.Format != StorageVersion || record.ID != id || !validLaunchTicket(record.Ticket) {
			return ErrRecovery
		}
		switch record.Outcome {
		case "unknown":
			return ErrLockPending
		case "spawned", "failed", "canceled":
			// Terminal metadata contains no secret and is no longer needed for lock
			// recovery. Request receipts remain responsible for operation idempotency.
			if os.Remove(filepath.Join(dir, entry.Name())) != nil {
				return ErrRecovery
			}
		default:
			return ErrRecovery
		}
	}
	return nil
}
