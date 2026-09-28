package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/maintenance"
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

func catalogError(code string) *protocol.Error {
	exit := 1
	if code == "invalid_storage" {
		exit = 3
	}
	return protocol.NewError(code, "Cannot enumerate profile storage.", exit, nil)
}

func (c Catalog) acquire(ctx context.Context) (func(), *protocol.Error) {
	if !filepath.IsAbs(c.Data) {
		return nil, catalogError("storage_error")
	}
	release, err := maintenance.Acquire(ctx, c.Data)
	if err != nil {
		if ctx.Err() != nil {
			return nil, protocol.NewError("canceled", "Request canceled.", 130, nil)
		}
		return nil, catalogError("storage_error")
	}
	return release, nil
}

// storedProfileNames requires the data-root maintenance gate.
func storedProfileNames(directory string) ([]string, *protocol.Error) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, catalogError("storage_error")
	}
	domain := "tasks"
	if filepath.Base(directory) == "profiles" {
		domain = "values"
	}
	items, err := profilekey.Enumerate(directory, domain)
	if err != nil {
		return nil, catalogError("invalid_storage")
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
	portState, err := (ports.Store{Directory: filepath.Join(c.Data, "ports")}).Read()
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
	items := make([]string, 0, len(seen))
	for profile := range seen {
		items = append(items, profile)
	}
	sort.Strings(items)
	return items, nil
}
