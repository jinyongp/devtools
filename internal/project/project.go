// Package project resolves tracked project configuration within worktree boundaries.
package project

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/pelletier/go-toml/v2"
)

const Filename = "devtools.toml"

type Config struct {
	Ports        map[string]Port    `toml:"ports,omitempty"`
	Requirements Requirements       `toml:"requirements,omitempty"`
	Profile      string             `toml:"profile"`
	Commands     map[string]Command `toml:"commands,omitempty"`
}

type Command struct {
	Serve        []string           `toml:"serve,omitempty"`
	Bind         map[string]Binding `toml:"bind,omitempty"`
	Requirements Requirements       `toml:"requirements,omitempty"`
	Exec         []string           `toml:"exec"`
	Inject       bool               `toml:"inject,omitempty"`
	Env          string             `toml:"env,omitempty"`
}

type Context struct {
	Ports        map[string]Port    `json:"-"`
	Requirements Requirements       `json:"-"`
	Profile      string             `json:"profile"`
	Source       string             `json:"source"`
	ConfigPath   string             `json:"config_path,omitempty"`
	Root         string             `json:"root,omitempty"`
	Commands     map[string]Command `json:"-"`
}

const ProfilePattern = `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`

var profilePattern = regexp.MustCompile(ProfilePattern)

func ValidProfile(profile string) bool { return profilePattern.MatchString(profile) }

// Resolve gives an explicit profile precedence over filesystem configuration.
// Otherwise it searches upward, including but never crossing a .git boundary.
func Resolve(start, explicitProfile string) (Context, *protocol.Error) {
	if explicitProfile != "" {
		if !ValidProfile(explicitProfile) {
			return Context{}, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, map[string]any{"field": "profile"})
		}
		return Context{Profile: explicitProfile, Source: "flag"}, nil
	}
	dir, err := filepath.Abs(start)
	if err == nil {
		dir, err = filepath.EvalSymlinks(dir)
	}
	if err != nil {
		return Context{}, ioError(start)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return Context{}, ioError(dir)
	}
	if !info.IsDir() {
		return Context{}, protocol.NewError("invalid_argument", "Project search requires a directory.", 2, map[string]any{"path": dir})
	}
	for {
		path := filepath.Join(dir, Filename)
		data, err := os.ReadFile(path)
		if err == nil {
			return parse(data, path, dir)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Context{}, ioError(path)
		}
		_, err = os.Lstat(filepath.Join(dir, ".git"))
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Context{}, ioError(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return Context{}, protocol.NewError("project_not_found", "No devtools.toml found. Add project configuration or pass --profile.", 3, map[string]any{"path": start})
}

func parse(data []byte, path, root string) (Context, *protocol.Error) {
	var config Config
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Context{}, protocol.NewError("invalid_config", "Invalid TOML or unknown configuration fields.", 3, map[string]any{"path": path})
	}
	if !ValidProfile(config.Profile) {
		return Context{}, protocol.NewError("invalid_config", "A valid profile identifier is required.", 3, map[string]any{"path": path, "field": "profile"})
	}
	for name, command := range config.Commands {
		if !validBindings(command, config.Ports) {
			return Context{}, protocol.NewError("invalid_config", "Invalid port, serve, or bind declaration.", 3, nil)
		}
		if !command.Requirements.Valid() || !config.Requirements.Merge(command.Requirements).Valid() {
			return Context{}, protocol.NewError("invalid_config", "Invalid command requirements.", 3, map[string]any{"path": path, "field": "commands"})
		}
		if !ValidProfile(name) || len(command.Exec) == 0 || command.Exec[0] == "" || (command.Env != "" && !ValidProfile(command.Env)) {
			return Context{}, protocol.NewError("invalid_config", "Invalid command definition.", 3, map[string]any{"path": path, "field": "commands"})
		}
		for _, arg := range command.Exec {
			if strings.ContainsRune(arg, 0) {
				return Context{}, protocol.NewError("invalid_config", "Command arguments contain a NUL byte.", 3, map[string]any{"path": path, "field": "commands"})
			}
		}
	}
	if !config.Requirements.Valid() {
		return Context{}, protocol.NewError("invalid_config", "Invalid project requirements. Use exact tool versions and distinct variable/secret keys.", 3, map[string]any{"path": path, "field": "requirements"})
	}
	for name, port := range config.Ports {
		if !ValidProfile(name) || !port.Valid() {
			return Context{}, protocol.NewError("invalid_config", "Invalid port declaration.", 3, nil)
		}
	}
	return Context{Profile: config.Profile, Source: "file", ConfigPath: path, Root: root, Commands: config.Commands, Requirements: config.Requirements, Ports: config.Ports}, nil
}

func ioError(path string) *protocol.Error {
	return protocol.NewError("io_error", "Cannot read project configuration or directory.", 1, map[string]any{"path": path})
}
