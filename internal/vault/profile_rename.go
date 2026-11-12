package vault

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/profilekey"
)

// ProfileRenameFilesHeld prepares existing profile state for a change of
// address. The caller must hold the exclusive maintenance gate and, for a
// protected root, the exclusive launch lease and authenticated catalog key.
func (s FileStore) ProfileRenameFilesHeld(ctx context.Context, source, target string) (map[string][]byte, error) {
	if source == target || !namePattern.MatchString(source) || !namePattern.MatchString(target) {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory := filepath.Join(s.Root, "profiles")
	read := func(name string) (*Profile, *Material, error) {
		if s.CatalogKey != nil {
			return s.ReadAnyWithMasterHeld(ctx, name, s.CatalogKey)
		}
		body, err := profilekey.Snapshot(directory, name, "values", MaxDocumentBytes)
		if err != nil {
			return nil, nil, err
		}
		if body == nil {
			return nil, nil, errorsNamed("profile_not_found")
		}
		defer clear(body)
		p, err := DecodeProfile(body)
		if err != nil {
			return nil, nil, err
		}
		if p.Name != name {
			return nil, nil, ErrIntegrity
		}
		if p.Sealed {
			return nil, nil, ErrKeyMissing
		}
		return p, nil, p.Verify(nil)
	}
	if s.CatalogKey == nil {
		if err := CheckPublicRootHeld(s.Root); err != nil {
			return nil, err
		}
	}
	missing := func(err error) bool {
		var condition *valueError
		return errors.As(err, &condition) && condition.code == "profile_not_found"
	}
	_, existingKey, err := read(target)
	if existingKey != nil {
		existingKey.Destroy()
	}
	if err == nil {
		return nil, errorsNamed("profile_exists")
	}
	if !missing(err) {
		return nil, err
	}
	profile, key, err := read(source)
	if missing(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if key != nil {
		defer key.Destroy()
	}
	oldFiles, err := profilekey.Replacement(directory, source, "values", nil)
	if err != nil {
		return nil, err
	}
	profile.Name = target
	if err := profile.Sign(key); err != nil {
		return nil, err
	}
	if s.CatalogKey != nil {
		catalog, err := s.readCatalog()
		if err != nil {
			return nil, err
		}
		for i, entry := range catalog.Entries {
			if entry.Name == source {
				catalog.Entries[i] = entryFor(profile)
				break
			}
		}
		return s.publicationFiles(ctx, catalog, profile, key, oldFiles)
	}
	body, err := profile.Encode()
	if err != nil {
		return nil, err
	}
	files, err := profilekey.Replacement(directory, target, "values", body)
	if err != nil {
		return nil, err
	}
	for path, content := range oldFiles {
		files[path] = content
	}
	if err := AddCommitHint(files, profile); err != nil {
		return nil, err
	}
	return files, nil
}
