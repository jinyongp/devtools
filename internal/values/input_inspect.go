package values

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// InspectInput authenticates current metadata without starting a worker,
// native helper or migration, decrypting secrets, or extending the key's idle
// deadline. Cold protection is deliberately an unavailable verification.
func InspectInput(ctx context.Context, root, profile, env string, spec InputSpec) (*InputVersion, *protocol.Error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !project.ValidProfile(profile) || (env != "" && !project.ValidProfile(env)) || !vault.ValidSelection(spec.Selection) {
		return nil, protocol.NewError("invalid_argument", "Invalid input inspection selection.", 2, nil)
	}
	if !spec.UsesValues() {
		v, err := NewInputVersion(SnapshotMetadata{}, env, spec)
		if err != nil {
			return nil, VaultError(ctx, err)
		}
		return v, nil
	}
	directory := filepath.Join(root, "vault")
	slot, keyErr := (vault.KeyFileStore{Directory: directory}).Read()
	var master *vault.Material
	if keyErr == nil {
		client, err := vault.OpenSessionClient(directory)
		if err != nil {
			return nil, VaultError(ctx, err)
		}
		master, err = client.InspectCached(ctx)
		if err != nil {
			return nil, VaultError(ctx, err)
		}
		defer master.Destroy()
	} else if !errors.Is(keyErr, vault.ErrKeyMissing) {
		return nil, VaultError(ctx, keyErr)
	}
	release, err := maintenance.AcquireShared(ctx, root)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	defer release()
	var p *vault.Profile
	var key *vault.Material
	absent := false
	if master != nil {
		p, key, err = (vault.FileStore{Root: root, VaultID: slot.VaultID, CatalogKey: master}).ReadAnyWithMasterHeld(ctx, profile, master)
		if err != nil {
			return nil, VaultError(ctx, err)
		}
		defer key.Destroy()
	} else {
		if err = vault.CheckPublicRootHeld(root); err != nil {
			return nil, VaultError(ctx, err)
		}
		raw, e := profilekey.Snapshot(filepath.Join(root, "profiles"), profile, "values", vault.MaxDocumentBytes)
		if e != nil {
			return nil, storageError()
		}
		defer clear(raw)
		if raw == nil {
			p, err = vault.NewProfile(profile)
			absent = true
		} else {
			p, err = vault.DecodeProfile(raw)
		}
		if err != nil {
			return nil, VaultError(ctx, err)
		}
		if p.Name != profile {
			return nil, VaultError(ctx, vault.ErrIntegrity)
		}
		if p.Sealed {
			return nil, VaultError(ctx, vault.ErrKeyMissing)
		}
	}

	effective, err := p.ResolveSelectedMetadata(env, key, spec.Selection)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	metadata := SnapshotMetadata{Identity: vault.InputIdentity{ProfileID: p.ID, StorageGeneration: p.Generation}, KeyEpoch: p.KeyEpoch, IntegrityVerified: master != nil, Slots: []vault.SlotReference{}}
	if absent {
		metadata.Identity = vault.InputIdentity{}
		metadata.Public = &vault.PublicLaunchTicket{Profile: profile}
	}
	for _, value := range effective {
		metadata.Slots = append(metadata.Slots, value.Reference)
	}
	byName := map[string]vault.ValueKind{}
	for _, value := range effective {
		byName[value.Reference.Name] = value.Kind
	}
	for _, name := range spec.Bindings {
		kind, exists := byName[name]
		if !exists {
			return nil, protocol.NewError("key_not_found", "A binding references a value outside the selected input.", 3, nil)
		}
		if kind != vault.Public {
			return nil, protocol.NewError("kind_conflict", "Bindings require public variables.", 3, nil)
		}
	}
	version, err := NewInputVersion(metadata, env, spec)
	if err != nil {
		return nil, VaultError(ctx, err)
	}
	for _, value := range effective {
		// Match preparation's selected-slot expiry rules without opening values.
		if (spec.Inject || value.Kind == vault.Public) && value.ExpiresAt != nil && time.Now().Unix() >= *value.ExpiresAt {
			return nil, protocol.NewError("value_expired", "A selected profile input has expired.", 3, nil)
		}
	}
	return version, nil
}
