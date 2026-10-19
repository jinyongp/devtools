package vault

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/profilekey"
)

// ReadAnyWithMasterHeld authenticates classification against the sealed catalog
// before accepting a separate unsigned public-only profile. Missing sealed data
// never falls back to a new or public profile.
func (s FileStore) ReadAnyWithMasterHeld(ctx context.Context, name string, master *Material) (*Profile, *Material, error) {
	p, key, err := s.ReadWithMasterHeld(ctx, name, master)
	if err == nil {
		return p, key, nil
	}
	if err.Error() != "profile_not_found" {
		return nil, nil, err
	}
	raw, err := profilekey.Snapshot(filepath.Join(s.Root, "profiles"), name, "values", MaxDocumentBytes)
	if err != nil {
		return nil, nil, ErrRecovery
	}
	if raw == nil {
		return nil, nil, errorsNamed("profile_not_found")
	}
	p, err = DecodeProfile(raw)
	if err != nil {
		return nil, nil, err
	}
	if p.Sealed {
		return nil, nil, ErrIntegrity
	}
	if err = p.Verify(nil); err != nil {
		return nil, nil, err
	}
	return p, nil, nil
}

// PublishManagedHeld is for ordinary value/env changes and first-secret
// promotion. Rotation/restore have their separate generation barriers. Callers
// validate the old profile under the same exclusive maintenance gate first.
func (s FileStore) PublishManagedHeld(ctx context.Context, p *Profile, key *Material) error {
	return s.PublishManagedWithFilesHeld(ctx, p, key, nil)
}
func (s FileStore) PublishManagedWithFilesHeld(ctx context.Context, p *Profile, key *Material, extra map[string][]byte) error {
	catalog, err := s.readCatalog()
	if err != nil {
		return err
	}
	index := -1
	for i, entry := range catalog.Entries {
		if entry.Name != p.Name {
			continue
		}
		index = i
		if entry.ID != p.ID || entry.Generation != p.Generation || p.Revision < entry.Revision {
			return ErrInputChanged
		}
		if entry.Sealed && !p.Sealed {
			return ErrIntegrity
		}
		if entry.KeyEpoch != p.KeyEpoch && !(entry.KeyEpoch == 0 && !entry.Sealed && p.Sealed && p.KeyEpoch == 1) {
			return ErrInputChanged
		}
	}
	if p.Sealed || index >= 0 {
		if index < 0 {
			catalog.Entries = append(catalog.Entries, entryFor(p))
		} else {
			catalog.Entries[index] = entryFor(p)
		}
	}
	return s.publishWithFiles(ctx, catalog, p, key, extra)
}
