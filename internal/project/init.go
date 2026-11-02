package project

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/pelletier/go-toml/v2"
)

type InitResult struct {
	Created         bool   `json:"created"`
	Existing        bool   `json:"-"`
	ConfigPath      string `json:"config_path"`
	Profile         string `json:"profile"`
	ProfileSource   string `json:"profile_source"`
	ProfilePresence string `json:"profile_presence"`
}

// Init retains the explicit-profile API for callers that already selected a name.
func Init(dir, profile string) (InitResult, *protocol.Error) {
	return PublishInit(dir, InitResult{Profile: profile, ProfileSource: "flag", ProfilePresence: "unchecked"}, true)
}

// InspectInit examines only the local configuration, before namespace lookup or
// prompting. An absent configuration gets the physical directory's basename.
func InspectInit(dir, profile string, explicit bool) (InitResult, *protocol.Error) {
	if explicit && !ValidProfile(profile) {
		return InitResult{}, protocol.NewError("invalid_argument", "A valid profile identifier is required.", 2, map[string]any{"field": "profile"})
	}
	root, err := location.ExistingDirectory(dir)
	if err != nil {
		return InitResult{}, initIOError(dir)
	}
	path := filepath.Join(root, Filename)
	result := InitResult{ConfigPath: path, Profile: profile, ProfileSource: "flag", ProfilePresence: "unchecked"}
	if _, err := os.Lstat(path); err == nil {
		return existingConfig(path, root, result, explicit)
	} else if !errors.Is(err, os.ErrNotExist) {
		return InitResult{}, initIOError(path)
	}
	if !explicit {
		result.Profile, result.ProfileSource = filepath.Base(root), "directory"
		if filepath.Dir(root) == root {
			result.Profile = ""
		}
	}
	return result, nil
}

// PublishInit links a complete file without replacing another initializer's
// result. Implicit selection adopts a concurrent valid configuration.
func PublishInit(dir string, selected InitResult, explicit bool) (InitResult, *protocol.Error) {
	result, failure := InspectInit(dir, selected.Profile, explicit)
	if failure != nil || result.Existing {
		return result, failure
	}
	if !ValidProfile(selected.Profile) {
		return InitResult{}, protocol.NewError("invalid_argument", "A valid profile identifier is required.", 2, map[string]any{"field": "profile"})
	}
	result.Profile, result.ProfileSource, result.ProfilePresence = selected.Profile, selected.ProfileSource, selected.ProfilePresence
	path, root := result.ConfigPath, filepath.Dir(result.ConfigPath)

	data, err := toml.Marshal(Config{Profile: result.Profile})
	if err != nil {
		return InitResult{}, initIOError(path)
	}
	file, err := os.CreateTemp(root, ".devtools-init-*")
	if err != nil {
		return InitResult{}, initIOError(path)
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return InitResult{}, initIOError(path)
	}
	if err := os.Link(file.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return existingConfig(path, root, result, explicit)
		}
		return InitResult{}, initIOError(path)
	}
	result.Created = true
	return result, nil
}

func existingConfig(path, root string, result InitResult, explicit bool) (InitResult, *protocol.Error) {
	info, err := os.Lstat(path)
	if err != nil {
		return InitResult{}, initIOError(path)
	}
	if !info.Mode().IsRegular() {
		return InitResult{}, protocol.NewError("invalid_config", "Existing configuration must be a regular file.", 3, map[string]any{"path": path})
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return InitResult{}, initIOError(path)
	}
	config, configErr := parse(data, path, root)
	if configErr != nil {
		return InitResult{}, configErr
	}
	if explicit && config.Profile != result.Profile {
		return InitResult{}, protocol.NewError("profile_conflict", "Existing configuration uses a different profile.", 3, map[string]any{"path": path, "field": "profile"})
	}
	result.Existing, result.Profile = true, config.Profile
	result.ProfileSource, result.ProfilePresence = "config", "unchecked"
	if explicit {
		result.ProfileSource = "flag"
	}
	return result, nil
}

func initIOError(path string) *protocol.Error {
	return protocol.NewError("io_error", "Cannot initialize project configuration.", 1, map[string]any{"path": path})
}
