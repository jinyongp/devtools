package values

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// CapturePublic pins actual values and opaque logical identities on a root
// without vault protection. Old storage requires explicit doctor migration.
func CapturePublic(ctx context.Context, root, profile, env string, inject bool, now time.Time, selection ...string) (*Snapshot, *protocol.Error) {
	if !project.ValidProfile(profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	p, exists, e := capturePublicProfile(ctx, root, profile)
	if e != nil {
		return nil, e
	}
	snapshot, e := captureProfile(p, nil, env, inject, now, selection...)
	if e != nil {
		return nil, e
	}
	ticket := vault.PublicLaunchTicket{Profile: profile}
	if exists {
		ticket.ProfileID = p.ID
		ticket.StorageGeneration = p.Generation
	} else {
		snapshot.metadata.Identity.ProfileID = ""
		snapshot.metadata.Identity.StorageGeneration = ""
		encoded := vault.NewEncoder("absent-effective-value-revision")
		encoded.String(profile)
		encoded.String(env)
		encoded.String(snapshot.metadata.Selection)
		metadata, e := encoded.Finish()
		if e != nil {
			snapshot.Destroy()
			return nil, VaultError(ctx, e)
		}
		sum := sha256.Sum256(metadata)
		snapshot.metadata.Identity.Revision = hex.EncodeToString(sum[:])
	}
	snapshot.metadata.Public = &ticket
	return snapshot, nil
}

// capturePublicProfile holds classification and profile reads under one gate.
func capturePublicProfile(ctx context.Context, root, profile string) (*vault.Profile, bool, *protocol.Error) {
	release, err := maintenance.AcquireShared(ctx, root)
	if err != nil {
		return nil, false, VaultError(ctx, err)
	}
	defer release()
	if err = vault.CheckPublicRootHeld(root); err != nil {
		return nil, false, VaultError(ctx, err)
	}
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: profile}
	p, exists, err := store.readPublicHeld()
	if err != nil {
		return nil, false, VaultError(ctx, err)
	}
	if p.Sealed {
		return nil, false, VaultError(ctx, vault.ErrKeyMissing)
	}
	return p, exists, nil
}
