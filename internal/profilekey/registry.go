package profilekey

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

const registryFile = "profile-names.json"
const registryLimit = 4 << 20

// Profile has a stable identity and storage address. Existing runtime clients
// continue to use Storage when a newer client changes Name.
type Profile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Storage string `json:"storage"`
}

type Registry struct {
	Version  int                `json:"version"`
	Names    map[string]string  `json:"names"`
	Profiles map[string]Profile `json:"profiles"`
}

// ReadRegistry is safe without a maintenance lease: publication replaces one
// complete file. Mutating callers hold the exclusive maintenance lease.
func ReadRegistry(root string) (Registry, *protocol.Error) {
	path := filepath.Join(root, registryFile)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return Registry{Version: 1, Names: map[string]string{}, Profiles: map[string]Profile{}}, nil
	} else if err != nil {
		return Registry{}, registryError(err)
	}
	body, err := maintenance.Read(path, registryLimit)
	if err != nil {
		return Registry{}, registryError(err)
	}
	var registry Registry
	if err := json.Unmarshal(body, &registry); err != nil || !registry.valid() {
		return Registry{}, registryError(errors.New("invalid profile name registry"))
	}
	return registry, nil
}

func (r Registry) valid() bool {
	if r.Version != 1 || r.Names == nil || r.Profiles == nil {
		return false
	}
	for id, profile := range r.Profiles {
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != id || profile.ID != id ||
			r.Names[profile.Name] != id || r.Names[profile.Storage] != id {
			return false
		}
	}
	for name, id := range r.Names {
		if !project.ValidProfile(name) || r.Profiles[id].ID != id || id == "" {
			return false
		}
	}
	return true
}

func (r Registry) Resolve(name string) Profile {
	if profile, ok := r.Profiles[r.Names[name]]; ok {
		return profile
	}
	return Profile{Name: name, Storage: name}
}

// Register adds an existing address while the caller owns the write lease.
func (r Registry) Register(storage string) (Profile, *protocol.Error) {
	if profile := r.Resolve(storage); profile.ID != "" {
		if profile.Storage != storage {
			return Profile{}, protocol.NewError("profile_storage_conflict", "An earlier client created separate data under a reserved profile name.", 3, map[string]any{"profile": storage})
		}
		return profile, nil
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Profile{}, registryError(err)
	}
	id := hex.EncodeToString(raw[:])
	if _, exists := r.Profiles[id]; exists {
		return Profile{}, registryError(errors.New("duplicate profile ID"))
	}
	profile := Profile{ID: id, Name: storage, Storage: storage}
	r.Profiles[id], r.Names[storage] = profile, id
	return profile, nil
}

func (r Registry) Publish(root string) *protocol.Error {
	if !r.valid() {
		return registryError(errors.New("invalid profile name registry"))
	}
	body, err := json.Marshal(r)
	if err != nil || len(body) > registryLimit {
		return registryError(errors.New("profile name registry exceeds its storage limit"))
	}
	if err := maintenance.Write(filepath.Join(root, registryFile), body); err != nil {
		return registryError(err)
	}
	return nil
}

func ResolveName(root, name string) (Profile, *protocol.Error) {
	registry, failure := ReadRegistry(root)
	if failure != nil {
		return Profile{}, failure
	}
	return registry.Resolve(name), nil
}

func ResolveProject(root, directory, explicit string) (project.Context, *protocol.Error) {
	p, failure := project.Resolve(directory, explicit)
	if failure != nil {
		return project.Context{}, failure
	}
	profile, failure := ResolveName(root, p.Profile)
	if failure != nil {
		return project.Context{}, failure
	}
	p.Profile = profile.Storage
	return p, nil
}

// CheckStorageAddress rejects a new client's write that selected an address
// just before it became an alias. Callers hold their ordinary write lock.
func CheckStorageAddress(ctx context.Context, root, storage string) *protocol.Error {
	if failure := protocol.ContextError(ctx.Err()); failure != nil {
		return failure
	}
	profile, failure := ResolveName(root, storage)
	if failure != nil {
		return failure
	}
	if profile.Storage != storage {
		return protocol.NewError("profile_binding_changed", "The profile name was assigned while preparing this operation. Retry the command.", 3, map[string]any{"profile": storage})
	}
	return nil
}

func registryError(err error) *protocol.Error {
	return protocol.WithStorageDetails(protocol.NewError("storage_error", "Cannot access the profile name registry.", 1, nil), "profiles", "profile_names", "profile_store", err)
}
