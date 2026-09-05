package project

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/pelletier/go-toml/v2"
)

type InitResult struct {
	Created    bool   `json:"created"`
	ConfigPath string `json:"config_path"`
	Profile    string `json:"profile"`
}

// Init publishes a complete configuration without replacing an existing path.
// Linking a prepared file makes concurrent initializers observe complete TOML.
func Init(dir, profile string) (InitResult, *protocol.Error) {
	if !ValidProfile(profile) {
		return InitResult{}, protocol.NewError("invalid_argument", "A valid profile identifier is required.", 2, map[string]any{"field": "profile"})
	}
	root, err := filepath.Abs(dir)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return InitResult{}, initIOError(dir)
	}
	path := filepath.Join(root, Filename)
	result := InitResult{ConfigPath: path, Profile: profile}
	if _, err := os.Lstat(path); err == nil {
		return existingConfig(path, root, result)
	} else if !errors.Is(err, os.ErrNotExist) {
		return InitResult{}, initIOError(path)
	}

	data, err := toml.Marshal(Config{Profile: profile})
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
			return existingConfig(path, root, result)
		}
		return InitResult{}, initIOError(path)
	}
	result.Created = true
	return result, nil
}

func existingConfig(path, root string, result InitResult) (InitResult, *protocol.Error) {
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
	if config.Profile != result.Profile {
		return InitResult{}, protocol.NewError("profile_conflict", "Existing configuration uses a different profile.", 3, map[string]any{"path": path, "field": "profile"})
	}
	return result, nil
}

func initIOError(path string) *protocol.Error {
	return protocol.NewError("io_error", "Cannot initialize project configuration.", 1, map[string]any{"path": path})
}
