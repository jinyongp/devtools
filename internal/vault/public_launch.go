package vault

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
)

// PublicLaunchTicket pins a public-only root's storage incarnation without
// requiring a key session. Both identities are empty only for an absent profile.
// It grants no secret access and cannot be used after vault initialization.
type PublicLaunchTicket struct {
	Profile           string `json:"profile"`
	ProfileID         string `json:"profile_id,omitempty"`
	StorageGeneration string `json:"storage_generation,omitempty"`
}

func (t PublicLaunchTicket) Valid() bool {
	return namePattern.MatchString(t.Profile) && ((t.ProfileID == "" && t.StorageGeneration == "") || (validID(t.ProfileID) && validID(t.StorageGeneration)))
}
func validLaunchRecord(r launchRecord) bool {
	if r.Public != nil {
		return r.Public.Valid() && r.Ticket == (LaunchTicket{})
	}
	return validLaunchTicket(r.Ticket)
}

// CheckPublicRootHeld closes a race with root initialization. Callers hold the
// data-root maintenance gate; no native key lookup or session interaction occurs.
func CheckPublicRootHeld(root string) error {
	if _, protected, err := maintenance.ProtectedRootHeld(root); err != nil {
		return ErrRecovery
	} else if protected {
		return ErrInputChanged
	}
	if _, err := (KeyFileStore{Directory: filepath.Join(root, "vault")}).Read(); err == nil {
		return ErrInputChanged
	} else if !errors.Is(err, ErrKeyMissing) {
		return err
	}
	if _, err := os.Lstat(filepath.Join(root, catalogRelative)); err == nil {
		return ErrKeyMissing
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrRecovery
	}
	return nil
}

// BeginPublicLaunch owns the same physical lease and durable outcome journal
// as a secret launch. Root initialization, promotion and restore must drain it
// before changing storage generation. The launcher owns it through actual spawn.
func BeginPublicLaunch(ctx context.Context, directory, id string, ticket PublicLaunchTicket) (*LaunchAdmission, error) {
	if !filepath.IsAbs(directory) || !validID(id) || !ticket.Valid() {
		return nil, ErrInvalid
	}
	release, err := maintenance.LockShared(ctx, launchGate(directory))
	if err != nil {
		return nil, ErrLockPending
	}
	admitted := false
	defer func() {
		if !admitted {
			release()
		}
	}()
	root := filepath.Dir(directory)
	gate, err := maintenance.AcquireShared(ctx, root)
	if err != nil {
		return nil, err
	}
	err = checkPublicTicketHeld(root, ticket)
	gate()
	if err != nil {
		return nil, err
	}
	r, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return nil, ErrRecovery
	}
	defer r.Close()
	if err = r.MkdirAll("launches"); err != nil {
		return nil, ErrRecovery
	}
	record := launchRecord{Format: StorageVersion, ID: id, Public: &ticket, Outcome: "unknown"}
	b, err := json.Marshal(record)
	if err != nil {
		return nil, ErrInvalid
	}
	if err = r.WriteFile(ctx, launchName(id), b, false); err != nil {
		return nil, ErrRecovery
	}
	admitted = true
	return &LaunchAdmission{directory: directory, record: record, release: release}, nil
}
func checkPublicTicketHeld(root string, t PublicLaunchTicket) error {
	if err := CheckPublicRootHeld(root); err != nil {
		return err
	}
	raw, err := profilekey.Snapshot(filepath.Join(root, "profiles"), t.Profile, "values", MaxDocumentBytes)
	if err != nil {
		return ErrRecovery
	}
	if raw == nil {
		if t.ProfileID == "" {
			return nil
		}
		return ErrInputChanged
	}
	p, err := DecodeProfile(raw)
	if err != nil {
		return err
	}
	if p.Sealed || p.Name != t.Profile {
		return ErrIntegrity
	}
	if p.ID != t.ProfileID || p.Generation != t.StorageGeneration {
		return ErrInputChanged
	}
	return p.Verify(nil)
}
