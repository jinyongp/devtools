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
// admission and durable launcher draining are coordinated in launch.go; clearing
// this cache cannot revoke bytes already delivered to a caller or application.
type KeySession struct {
	mu           sync.Mutex
	directory    string
	key          *Material
	fingerprint  [sha256.Size]byte
	status       SessionStatus
	lastUse      time.Time
	idleTimer    *time.Timer
	unwrapGate   chan struct{}
	unwrapCancel context.CancelFunc
	epoch        uint64
	now          func() time.Time
	protector    func(KeyFile, *Material, bool) (Protector, error)
}

func NewKeySession(directory string) *KeySession {
	return &KeySession{directory: directory, status: SessionStatus{State: Locked}, now: time.Now, protector: sessionProtector, unwrapGate: make(chan struct{}, 1)}
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
	_, err := s.obtain(ctx, passphrase, interactive, true, nil)
	return err
}
func (s *KeySession) unlockAtEpoch(ctx context.Context, passphrase *Material, interactive bool, epoch uint64) error {
	_, err := s.obtain(ctx, passphrase, interactive, true, &epoch)
	return err
}
func (s *KeySession) lockEpoch() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.epoch }
func (s *KeySession) useAtEpoch(ctx context.Context, epoch uint64) (*Material, error) {
	return s.obtain(ctx, nil, false, false, &epoch)
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
	return s.obtain(ctx, nil, false, false, nil)
}

// Serialize derivation with a bounded semaphore, never the state mutex. Native
// IPC/KDF waits run outside mu; Lock can cancel them and invalidate the result.
func (s *KeySession) obtain(ctx context.Context, passphrase *Material, interactive, explicit bool, expectedEpoch *uint64) (*Material, error) {
	select {
	case s.unwrapGate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.unwrapGate }()
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if expectedEpoch != nil && *expectedEpoch != s.epoch {
		s.mu.Unlock()
		return nil, ErrLockPending
	}
	k, err := s.read()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if !explicit {
		if s.status.State == LockedManual {
			s.mu.Unlock()
			return nil, ErrLocked
		}
		if s.status.State == RecoveryRequired {
			s.mu.Unlock()
			return nil, ErrRecovery
		}
		if s.key != nil {
			defer s.mu.Unlock()
			return s.copyForUse()
		}
		if k.Backend == "passphrase" {
			s.mu.Unlock()
			return nil, ErrLocked
		}
	}
	epoch, fingerprint := s.epoch, s.fingerprint
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	s.unwrapCancel = cancel
	s.mu.Unlock()
	p, err := s.protector(*k, passphrase, interactive)
	var candidate *Material
	if err == nil {
		candidate, err = k.Unlock(work, k.Backend, p)
	}
	defer func() { candidate.Destroy() }()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unwrapCancel = nil
	if epoch != s.epoch {
		return nil, ErrLockPending
	}
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if _, err = s.read(); err != nil {
		return nil, err
	}
	if fingerprint != s.fingerprint {
		return nil, ErrRecovery
	}
	if !explicit && s.status.State == LockedManual {
		return nil, ErrLocked
	}
	if explicit {
		// Only a successful explicit unlock at the observed epoch clears intent.
		if err = s.latch(ctx, k, false); err != nil {
			s.destroy(RecoveryRequired)
			return nil, err
		}
	}
	s.key.Destroy()
	s.key = candidate
	candidate = nil
	if explicit {
		s.touch()
		return nil, nil
	}
	return s.copyForUse()
}
func (s *KeySession) copyForUse() (*Material, error) {
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
	if err := s.lockIntent(ctx); err != nil {
		return err
	}
	return DrainLaunches(ctx, s.directory)
}
func (s *KeySession) lockIntent(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.epoch++
	if s.unwrapCancel != nil {
		s.unwrapCancel()
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
func (s *KeySession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	if s.unwrapCancel != nil {
		s.unwrapCancel()
	}
	s.destroy(Locked)
}
