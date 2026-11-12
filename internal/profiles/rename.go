package profiles

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
)

type RenameResult struct {
	ProfileID string `json:"profile_id"`
	Previous  string `json:"previous_profile"`
	Profile   string `json:"profile"`
	Changed   bool   `json:"changed"`
}

func storageNameConflict(name string) *protocol.Error {
	return protocol.NewError("profile_storage_conflict", "An earlier client created separate data under a reserved profile name.", 3, map[string]any{"profile": name})
}

func (c Catalog) Rename(ctx context.Context, from, to string) (RenameResult, *protocol.Error) {
	var result RenameResult
	if !filepath.IsAbs(c.Data) || from == to || !project.ValidProfile(from) || !project.ValidProfile(to) {
		return result, protocol.NewError("invalid_argument", "Supply two different valid profile names.", 2, nil)
	}
	release, err := maintenance.AcquireExclusive(ctx, c.Data)
	if err != nil {
		if failure := protocol.ContextError(ctx.Err()); failure != nil {
			return result, failure
		}
		return result, catalogError("storage_error", err)
	}
	defer release()
	releasePorts, failure := (ports.Store{Directory: filepath.Join(c.Data, "ports")}).LockRegistry(ctx)
	if failure != nil {
		return result, failure
	}
	defer releasePorts()
	releasePublication, failure := (services.Store{Data: c.Data}).LockPublication(ctx)
	if failure != nil {
		return result, failure
	}
	defer releasePublication()
	registry, failure := profilekey.ReadRegistry(c.Data)
	if failure != nil {
		return result, failure
	}
	storage, failure := c.storageNamesHeld(ctx)
	if failure != nil {
		return result, failure
	}
	for _, name := range storage {
		if _, failure := registry.Register(name); failure != nil {
			return result, failure
		}
	}
	source, target := registry.Resolve(from), registry.Resolve(to)
	if source.ID == "" {
		return result, protocol.NewError("profile_not_found", "The source profile does not exist.", 3, map[string]any{"profile": from})
	}
	result = RenameResult{ProfileID: source.ID, Previous: from, Profile: to}
	if source.Name == to {
		return result, nil
	}
	if source.Name != from {
		return result, protocol.NewError("profile_renamed", "Use the current profile name as the source of a rename.", 3, map[string]any{"profile": from, "current_profile": source.Name})
	}
	if target.ID != "" && target.ID != source.ID {
		return result, protocol.NewError("profile_exists", "The target name already belongs to another profile.", 3, map[string]any{"profile": to})
	}
	if failure := protocol.ContextError(ctx.Err()); failure != nil {
		return result, failure
	}
	source.Name = to
	registry.Names[to], registry.Profiles[source.ID] = source.ID, source
	if failure := registry.Publish(c.Data); failure != nil {
		return result, failure
	}
	result.Changed = true
	return result, nil
}
