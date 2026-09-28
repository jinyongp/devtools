package profilekey

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
)

// Snapshot reads logical state while the caller holds the maintenance gate.
// A missing domain returns nil; migration markers are never snapshot payloads.
func Snapshot(directory, profile, domain string, limit int64) ([]byte, error) {
	if !project.ValidProfile(profile) || domain != "values" && domain != "tasks" {
		return nil, errors.New("invalid profile snapshot")
	}
	if err := privateDirectory(directory); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	resolved, err := Resolve(directory, profile, domain)
	if err != nil {
		return nil, err
	}
	var path string
	switch resolved.Mode {
	case ModeNone:
		return nil, nil
	case ModeLegacy:
		path = resolved.LegacyPath
	case ModeCanonical:
		path = resolved.CanonicalPath
	default:
		return nil, errors.New("invalid profile snapshot mode")
	}
	body, err := maintenance.Read(path, limit)
	if err != nil {
		return nil, err
	}
	var identity struct {
		Profile string `json:"profile"`
	}
	if json.Unmarshal(body, &identity) != nil || identity.Profile != profile {
		return nil, errors.New("snapshot profile mismatch")
	}
	return body, nil
}

// Replacement returns the complete logical create/replace/delete set for a
// domain. The caller must validate domain payloads and publish this set with
// maintenance.Replace while holding its exclusive gate.
func Replacement(directory, profile, domain string, data []byte) (map[string][]byte, error) {
	if !project.ValidProfile(profile) || domain != "values" && domain != "tasks" {
		return nil, errors.New("invalid profile replacement")
	}
	storageDomain := "tasks"
	if domain == "values" {
		storageDomain = "profiles"
	}
	if filepath.Base(directory) != storageDomain {
		return nil, errors.New("invalid domain directory")
	}
	files := map[string][]byte{}
	if err := privateDirectory(directory); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if data == nil {
			return files, nil
		}
		if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := privateDirectory(directory); err != nil {
			return nil, err
		}
	}
	resolved, err := Resolve(directory, profile, domain)
	if err != nil {
		return nil, err
	}
	if data == nil {
		if resolved.Mode == ModeCanonical {
			files[StateRelative(storageDomain, profile)] = nil
			files[IdentityRelative(storageDomain, profile)] = nil
		}
		if resolved.LegacyStatus == LegacyExisting {
			files[LegacyRelative(storageDomain, profile)] = nil
		}
		return files, nil
	}
	var stored struct {
		Profile string `json:"profile"`
	}
	if json.Unmarshal(data, &stored) != nil || stored.Profile != profile {
		return nil, errors.New("replacement profile mismatch")
	}
	identity, err := IdentityBytes(profile, domain)
	if err != nil {
		return nil, err
	}
	files[StateRelative(storageDomain, profile)] = data
	files[IdentityRelative(storageDomain, profile)] = identity
	if resolved.LegacyStatus != LegacyUnaddressable {
		marker, err := MarkerBytes(profile)
		if err != nil {
			return nil, err
		}
		files[LegacyRelative(storageDomain, profile)] = marker
	}
	return files, nil
}
