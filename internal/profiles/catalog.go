package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
)

// Catalog discovers logical identities without contacting supervisors. Each
// public entry point holds one maintenance gate across its domain reads.
type Catalog struct {
	Data string
}

func catalogError(code string, causes ...error) *protocol.Error {
	exit := 1
	if code == "invalid_storage" {
		exit = 3
	}
	var cause error
	if len(causes) != 0 {
		cause = causes[0]
	}
	failure := protocol.WithStorageDetails(protocol.NewError(code, "Cannot enumerate profile storage.", exit, nil), "profiles", "enumerate_profiles", "profile_store", cause)
	var preparation *maintenance.QueryPreparationError
	if errors.As(cause, &preparation) {
		return protocol.WithStoragePreparation(failure, preparation.State, preparation.Passive)
	}
	return failure
}

func (c Catalog) acquire(ctx context.Context) (func(), *protocol.Error) {
	if !filepath.IsAbs(c.Data) {
		return nil, catalogError("storage_error")
	}
	release, err := maintenance.AcquireQuery(ctx, c.Data)
	if err != nil {
		return nil, catalogError("storage_error", err)
	}
	return release, nil
}

// storedProfileNames requires the data-root maintenance gate.
func storedProfileNames(directory string) ([]string, *protocol.Error) {
	_, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, catalogError("storage_error", err)
	}
	if err := platformfs.CheckDirectory(directory); err != nil {
		return nil, catalogError("storage_error", err)
	}
	domain := "tasks"
	if filepath.Base(directory) == "profiles" {
		domain = "values"
	}
	items, err := profilekey.Enumerate(directory, domain)
	if err != nil {
		return nil, catalogError("invalid_storage", err)
	}
	return items, nil
}

func (c Catalog) Names(ctx context.Context) ([]string, *protocol.Error) {
	release, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return c.namesHeld(ctx)
}

// CompletionNames reuses profile discovery across values, tasks, ports and
// processes, without initializing missing locks or recovering storage on Tab.
func (c Catalog) CompletionNames(ctx context.Context) ([]string, *protocol.Error) {
	release, err := maintenance.AcquirePassiveQuery(ctx, c.Data)
	if err != nil {
		return nil, catalogError("storage_error", err)
	}
	defer release()
	return c.namesHeld(ctx)
}

// WithPresence uses metadata only and holds the existing query gate while the
// caller publishes its local configuration. It never bootstraps a data root.
func (c Catalog) WithPresence(ctx context.Context, name string, use func(bool) *protocol.Error) *protocol.Error {
	if failure := protocol.ContextError(ctx.Err()); failure != nil {
		return failure
	}
	if !filepath.IsAbs(c.Data) {
		return catalogError("storage_error")
	}
	if _, err := os.Lstat(c.Data); errors.Is(err, os.ErrNotExist) {
		return use(false)
	} else if err != nil {
		return catalogError("storage_error", err)
	}
	if err := platformfs.CheckDirectory(c.Data); err != nil {
		return catalogError("storage_error", err)
	}
	entries, err := os.ReadDir(c.Data)
	if err != nil {
		return catalogError("storage_error", err)
	}
	if len(entries) == 0 {
		return use(false)
	}
	release, err := maintenance.AcquirePassiveQuery(ctx, c.Data)
	if err != nil {
		return catalogError("storage_error", err)
	}
	defer release()
	if condition := profilekey.RequireCurrentHeld(c.Data, name); condition != nil {
		return condition
	}
	names, failure := c.namesHeld(ctx)
	if failure != nil {
		return failure
	}
	present := false
	for _, profile := range names {
		for _, domain := range []string{"values", "tasks"} {
			directory := "profiles"
			if domain == "tasks" {
				directory = "tasks"
			}
			if _, err := os.Lstat(filepath.Join(c.Data, directory)); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return catalogError("storage_error", err)
			}
			resolution, err := profilekey.Resolve(filepath.Join(c.Data, directory), profile, domain)
			if err != nil {
				return catalogError("invalid_storage", err)
			}
			if resolution.Mode == profilekey.ModeLegacy {
				return protocol.WithStoragePreparation(protocol.NewError("migration_required", "Storage conversion is required.", 3, nil), "migration_required", true)
			}
		}
		present = present || profile == name
	}
	return use(present)
}

func (c Catalog) namesHeld(ctx context.Context) ([]string, *protocol.Error) {
	seen := map[string]bool{}
	add := func(items []string) {
		for _, item := range items {
			seen[item] = true
		}
	}
	for _, domain := range []string{"profiles", "tasks"} {
		if ctx.Err() != nil {
			return nil, protocol.NewError("canceled", "Request canceled.", 130, nil)
		}
		items, err := storedProfileNames(filepath.Join(c.Data, domain))
		if err != nil {
			return nil, err
		}
		add(items)
	}
	portState, err := ports.ForDataRoot(c.Data).ReadHeld()
	if err != nil {
		return nil, err
	}
	for _, instance := range portState.Instances {
		seen[instance.Profile] = true
	}
	processProfiles, processErr := (services.Store{Data: c.Data}).ProfileNames()
	if processErr != nil {
		return nil, processErr
	}
	add(processProfiles)
	renames, readErr := profilekey.ReadRenameNamesHeld(c.Data)
	if readErr != nil {
		return nil, catalogError("invalid_storage", readErr)
	}
	for _, current := range renames.Current {
		seen[current] = true
	}
	items := make([]string, 0, len(seen))
	for profile := range seen {
		if renames.CurrentName(profile) == profile {
			items = append(items, profile)
		}
	}
	sort.Strings(items)
	return items, nil
}
