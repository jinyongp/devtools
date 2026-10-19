package values

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// CapturePublic pins actual values and opaque logical identities on a root
// without vault protection. Legacy public-only profiles are converted once;
// legacy secrets require explicit protected migration. No key session is opened.
func CapturePublic(ctx context.Context, root, profile, env string, inject bool, now time.Time) (*Snapshot, *protocol.Error) {
	if !project.ValidProfile(profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	defer release()
	if err = vault.CheckPublicRootHeld(root); err != nil {
		return nil, VaultError(ctx, err)
	}
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: profile}
	raw, err := profilekey.Snapshot(store.Directory, profile, "values", vault.MaxDocumentBytes)
	if err != nil {
		return nil, storageError()
	}
	defer clear(raw)
	p, exists, err := store.readPublicHeld()
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	if p.Sealed {
		return nil, VaultError(ctx, vault.ErrKeyMissing)
	}
	// A converted legacy profile needs durable IDs, so subsequent executions and
	// storage replacement can distinguish value revisions from incarnations.
	if exists {
		if _, err = vault.DecodeProfile(raw); err != nil {
			if err = store.publishHeld(ctx, p, nil, nil, ""); err != nil {
				return nil, VaultError(ctx, err)
			}
		}
	}
	snapshot, e := captureProfile(p, nil, env, inject, now)
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
