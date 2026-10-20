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
func CapturePublic(ctx context.Context, root, profile, env string, inject bool, now time.Time, selection ...string) (*Snapshot, *protocol.Error) {
	if !project.ValidProfile(profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	p, exists, needsExclusive, e := capturePublicProfile(ctx, root, profile, false)
	if e != nil {
		return nil, e
	}
	if needsExclusive {
		p, exists, _, e = capturePublicProfile(ctx, root, profile, true)
		if e != nil {
			return nil, e
		}
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
func capturePublicProfile(ctx context.Context, root, profile string, exclusive bool) (*vault.Profile, bool, bool, *protocol.Error) {
	acquire := maintenance.AcquireShared
	if exclusive {
		acquire = maintenance.AcquireExclusive
	}
	release, err := acquire(ctx, root)
	if err != nil {
		return nil, false, false, VaultError(ctx, err)
	}
	defer release()
	if err = vault.CheckPublicRootHeld(root); err != nil {
		return nil, false, false, VaultError(ctx, err)
	}
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: profile}
	raw, err := profilekey.Snapshot(store.Directory, profile, "values", vault.MaxDocumentBytes)
	if err != nil {
		return nil, false, false, storageErrorAt("read_profile", err)
	}
	defer clear(raw)
	p, exists, err := store.readPublicHeld()
	if err != nil {
		return nil, false, false, VaultError(ctx, err)
	}
	if p.Sealed {
		return nil, false, false, VaultError(ctx, vault.ErrKeyMissing)
	}
	// A converted legacy profile needs durable IDs, so subsequent executions and
	// storage replacement can distinguish value revisions from incarnations.
	if exists {
		if _, err = vault.DecodeProfile(raw); err != nil {
			if !exclusive {
				return nil, false, true, nil
			}
			if err = store.publishHeld(ctx, p, nil, nil, ""); err != nil {
				return nil, false, false, VaultError(ctx, err)
			}
		}
	}
	return p, exists, false, nil
}
