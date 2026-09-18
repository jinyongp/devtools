package profiles

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
)

// Catalog discovers profile identities across the user-global devtools data
// root. It is intentionally passive: listing profiles does not contact live
// process supervisors or perform readiness checks.
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

func storedProfileNames(directory string) ([]string, *protocol.Error) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, catalogError("storage_error")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, catalogError("storage_error")
	}
	items := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		encoded := strings.TrimSuffix(name, ".json")
		decoded, decodeErr := hex.DecodeString(encoded)
		profile := string(decoded)
		if decodeErr != nil || !project.ValidProfile(profile) {
			return nil, catalogError("invalid_storage")
		}
		fileInfo, statErr := os.Lstat(filepath.Join(directory, name))
		if statErr != nil || !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm()&0077 != 0 {
			return nil, catalogError("storage_error")
		}
		items = append(items, profile)
	}
	return items, nil
}

func (c Catalog) Names() ([]string, *protocol.Error) {
	if !filepath.IsAbs(c.Data) {
		return nil, catalogError("storage_error")
	}
	seen := map[string]bool{}
	add := func(items []string) {
		for _, item := range items {
			seen[item] = true
		}
	}
	for _, domain := range []string{"profiles", "tasks"} {
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
