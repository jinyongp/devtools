package cli

import (
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) resolveStoredProject(directory, explicit string) (project.Context, *protocol.Error) {
	profiles, failure := a.dataDirectory()
	if failure != nil {
		return project.Context{}, failure
	}
	return profilekey.ResolveProject(filepath.Dir(profiles), directory, explicit)
}

func (a *App) resolveProfileOption(command string, r Request) *protocol.Error {
	name := r.Options["profile"]
	// These selectors are names in an archive or literal configuration inputs.
	if name == "" || command == "profile import" || command == "backup restore" || command == "init" || strings.HasPrefix(command, "config ") {
		return nil
	}
	profiles, failure := a.dataDirectory()
	if failure != nil {
		return failure
	}
	profile, failure := profilekey.ResolveName(filepath.Dir(profiles), name)
	if failure != nil {
		return failure
	}
	r.Options["profile"] = profile.Storage
	return nil
}

// Public results carry names even when older clients still use the backing key.
// Limit the projection to result and row metadata; stored payloads remain opaque.
func (a *App) displayProfileNames(command string, result any) any {
	if command == "profile rename" || command == "profile list" || command == "profile inspect" || command == "init" || strings.HasPrefix(command, "config ") {
		return result
	}
	profiles, failure := a.dataDirectory()
	if failure != nil {
		return result
	}
	registry, failure := profilekey.ReadRegistry(filepath.Dir(profiles))
	if failure != nil || len(registry.Names) == 0 {
		return result
	}
	object := publicResultObject(result)
	if object == nil {
		return result
	}
	projectName := func(row map[string]any) {
		if profile, ok := row["profile"].(string); ok {
			row["profile"] = registry.Resolve(profile).Name
		}
	}
	projectName(object)
	if item, ok := object["item"].(map[string]any); ok {
		projectName(item)
	}
	if rows, ok := object["items"].([]any); ok {
		for _, row := range rows {
			if item, ok := row.(map[string]any); ok {
				projectName(item)
				if nested, ok := item["item"].(map[string]any); ok {
					projectName(nested)
				}
			}
		}
	}
	if command == "backup restore" {
		if targets, ok := object["targets"].([]any); ok {
			for _, row := range targets {
				if target, ok := row.(map[string]any); ok {
					projectName(target)
				}
			}
		}
	}
	return object
}
