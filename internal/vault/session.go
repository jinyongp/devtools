package vault

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

type SessionStatus struct {
	State         SessionState `json:"state"`
	VaultID       string       `json:"vault_id,omitempty"`
	Backend       string       `json:"backend,omitempty"`
	IdleExpiresAt *time.Time   `json:"idle_expires_at,omitempty"`
}
type manualLock struct {
	Format  int    `json:"format"`
	VaultID string `json:"vault_id"`
	Locked  bool   `json:"locked"`
}

// KeySession owns one root key and serializes bounded unwrap/KDF work. Execution
// admission and launcher draining belong to the execution layer (A08); clearing
// this cache cannot revoke bytes already delivered to a caller or application.
type KeySession struct {
	mu          sync.Mutex
	directory   string
	key         *Material
	fingerprint [sha256.Size]byte
	status      SessionStatus
	lastUse     time.Time
	idleTimer   *time.Timer
	now         func() time.Time
	protector   func(KeyFile, *Material, bool) (Protector, error)
}

func NewKeySession(directory string) *KeySession {
	return &KeySession{directory: directory, status: SessionStatus{State: Locked}, now: time.Now, protector: sessionProtector}
}
func sessionProtector(k KeyFile, passphrase *Material, interactive bool) (Protector, error) {
	if k.Backend == "passphrase" {
		return PassphraseProtector{VaultID: k.VaultID, Passphrase: passphrase}, nil
	}
	return sessionNativeProtector{keyfile: k, interactive: interactive}, nil
}
func (s *KeySession) destroy(state SessionState) {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	s.key.Destroy()
	s.key = nil
	s.lastUse = time.Time{}
	s.status.IdleExpiresAt = nil
	s.status.State = state
}
func (s *KeySession) read() (*KeyFile, error) {
	k, err := (KeyFileStore{Directory: s.directory}).Read()
	if err != nil {
		if errors.Is(err, ErrKeyMissing) {
			s.destroy(Uninitialized)
		} else {
			s.destroy(RecoveryRequired)
		}
		return nil, err
	}
	s.status.Backend = k.Backend
	s.status.VaultID = k.VaultID
	b, _ := json.Marshal(k)
	digest := sha256.Sum256(b)
	if s.key != nil && digest != s.fingerprint {
		s.destroy(Locked)
	}
	s.fingerprint = digest
	r, err := platformfs.OpenRoot(s.directory, false)
	if err != nil {
		s.destroy(RecoveryRequired)
		return nil, ErrRecovery
	}
	defer r.Close()
	manual, err := r.ReadFile("manual-lock.json", MaxKeySlotBytes)
	if err == nil {
		var latch manualLock
		if StrictDecode(manual, &latch) != nil || latch.Format != StorageVersion || latch.VaultID != k.VaultID {
			s.destroy(RecoveryRequired)
			return nil, ErrRecovery
		}
		if latch.Locked {
			s.destroy(LockedManual)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		s.destroy(RecoveryRequired)
		return nil, ErrRecovery
	}
	if s.key != nil && !s.now().Before(s.lastUse.Add(DefaultIdleTimeout)) {
		s.destroy(Locked)
	}
	if s.status.State == Uninitialized {
		s.status.State = Locked
	}
	return k, nil
}
func (s *KeySession) latch(ctx context.Context, k *KeyFile, locked bool) error {
	r, err := platformfs.OpenRoot(s.directory, false)
	if err != nil {
		return ErrRecovery
	}
	defer r.Close()
	b, _ := json.Marshal(manualLock{Format: StorageVersion, VaultID: k.VaultID, Locked: locked})
	if err = r.WriteFile(ctx, "manual-lock.json", b, true); err != nil {
		return ErrRecovery
	}
	return nil
}
func (s *KeySession) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.read()
	status := s.status
	if status.IdleExpiresAt != nil {
		expires := *status.IdleExpiresAt
		status.IdleExpiresAt = &expires
	}
	return status
}
func (s *KeySession) Unlock(ctx context.Context, passphrase *Material, interactive bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	k, err := s.read()
	if err != nil {
		return err
	}
	p, err := s.protector(*k, passphrase, interactive)
	if err != nil {
		return err
	}
	key, err := k.Unlock(ctx, k.Backend, p)
	if err != nil {
		return err
	}
	// Only an explicit, successful unlock clears the durable manual latch.
	if err = s.latch(ctx, k, false); err != nil {
		key.Destroy()
		s.destroy(RecoveryRequired)
		return err
	}
	s.key.Destroy()
	s.key = key
	s.touch()
	return nil
}
func (s *KeySession) touch() {
	s.lastUse = s.now()
	expires := s.lastUse.Add(DefaultIdleTimeout)
	s.status.State = Unlocked
	s.status.IdleExpiresAt = &expires
	s.scheduleExpiry()
}

// Expire the owned key even when no client polls the daemon. The read path also
// checks expiry, so timer scheduling delay cannot admit an expired key.
func (s *KeySession) scheduleExpiry() {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	delay := s.lastUse.Add(DefaultIdleTimeout).Sub(s.now())
	s.idleTimer = time.AfterFunc(delay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.key == nil {
			return
		}
		if !s.now().Before(s.lastUse.Add(DefaultIdleTimeout)) {
			s.destroy(Locked)
		} else {
			s.scheduleExpiry()
		}
	})
}

// Use returns an owned key only to an authenticated internal operation. Polling
// Status never calls Use. Callers destroy the result after their bounded work.
func (s *KeySession) Use(ctx context.Context) (*Material, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k, err := s.read()
	if err != nil {
		return nil, err
	}
	if s.status.State == LockedManual {
		return nil, ErrLocked
	}
	if s.status.State == RecoveryRequired {
		return nil, ErrRecovery
	}
	if s.key == nil {
		if k.Backend == "passphrase" {
			return nil, ErrLocked
		}
		p, err := s.protector(*k, nil, false)
		if err != nil {
			return nil, err
		}
		key, err := k.Unlock(ctx, k.Backend, p)
		if err != nil {
			return nil, err
		}
		s.key = key
	}
	b := s.key.CopyBytes()
	defer clear(b)
	if len(b) != 32 {
		s.destroy(RecoveryRequired)
		return nil, ErrIntegrity
	}
	s.touch()
	return NewMaterial(b), nil
}
func (s *KeySession) Lock(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	k, err := s.read()
	if err != nil {
		return err
	}
	// Close cache admissions even if persisting the intent fails. An error is
	// never reported as successful durable lock.
	s.destroy(Locking)
	if err = s.latch(ctx, k, true); err != nil {
		s.destroy(RecoveryRequired)
		return err
	}
	s.destroy(LockedManual)
	return nil
}
func (s *KeySession) Close() { s.mu.Lock(); defer s.mu.Unlock(); s.destroy(Locked) }
