// Package vault owns protected local value storage and its key session.
// It does not run project code or claim to isolate programs owned by one user.
package vault

import (
	"context"
	"errors"
	"time"
)

const (
	StorageVersion  = 1
	SessionProtocol = 2
	// Keep the storage ceiling within the existing backup plaintext ceiling.
	MaxDocumentBytes   = 128 << 20
	MaxFrameBytes      = 16 << 20
	DefaultIdleTimeout = 8 * time.Hour
)

var (
	ErrInputChanged = errors.New("input_changed")
	ErrLocked       = errors.New("vault_locked")
	ErrKeyMissing   = errors.New("key_missing")
	ErrIntegrity    = errors.New("integrity_failed")
	ErrInvalid      = errors.New("invalid_vault_format")
	ErrTooLarge     = errors.New("vault_input_too_large")
	ErrUnavailable  = errors.New("key_backend_unavailable")
	ErrRecovery     = errors.New("vault_recovery_required")
	ErrLockPending  = errors.New("lock_pending")
)

// Protector wraps the random vault key. An implementation must never initialize
// a replacement key, change backend, or lower protection after an unwrap error.
type Protector interface {
	Identity() ProtectionIdentity
	Wrap(context.Context, []byte) ([]byte, error)
	Unwrap(context.Context, []byte) ([]byte, error)
}

type ProtectionIdentity struct {
	Backend string
	VaultID string
}

type SessionState string

const (
	Uninitialized    SessionState = "uninitialized"
	Locked           SessionState = "locked"
	Unlocked         SessionState = "unlocked"
	Locking          SessionState = "locking"
	LockedManual     SessionState = "locked_manual"
	RecoveryRequired SessionState = "recovery_required"
)

// InputIdentity is public execution metadata, never the environment payload.
// KeyEpoch is deliberately absent: re-encryption does not change application input.
type InputIdentity struct {
	ProfileID         string `json:"profile_id"`
	StorageGeneration string `json:"storage_generation"`
	Revision          string `json:"effective_input_revision"`
}
