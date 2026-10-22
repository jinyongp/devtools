// Package project resolves tracked project configuration within worktree boundaries.
package project

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/pelletier/go-toml/v2"
)

const Filename = "devtools.toml"

type Config struct {
	Tools        map[string]Tool    `toml:"tools,omitempty"`
	Presets      map[string]Preset  `toml:"presets,omitempty"`
	Defaults     Selection          `toml:"defaults,omitempty"`
	Bundle       string             `toml:"bundle,omitempty"`
	EnvFiles     []string           `toml:"env_files,omitempty"`
	Ports        map[string]Port    `toml:"ports,omitempty"`
	Proxies      map[string]Proxy   `toml:"proxies,omitempty"`
	Requirements Requirements       `toml:"requirements,omitempty"`
	Profile      string             `toml:"profile"`
	Commands     map[string]Command `toml:"commands,omitempty"`
}

type Command struct {
	Selection
	ToolOverrides map[string]Tool    `toml:"tool_overrides,omitempty"`
	ValuesUpdate  string             `toml:"values_update,omitempty"`
	Bundle        string             `toml:"bundle,omitempty"`
	Output        redact.Mode        `toml:"output,omitempty"`
	Stdout        redact.Mode        `toml:"stdout,omitempty"`
	Stderr        redact.Mode        `toml:"stderr,omitempty"`
	Ready         *ReadyProbe        `toml:"ready,omitempty"`
	Serve         []string           `toml:"serve,omitempty"`
	Bind          map[string]Binding `toml:"bind,omitempty"`
	Requirements  Requirements       `toml:"requirements,omitempty"`
	Exec          []string           `toml:"exec"`
	Inject        bool               `toml:"inject,omitempty"`
	Env           string             `toml:"env,omitempty"`
}

// ValueUpdatePolicy defaults to notification and never affects foreground runs.
func (c Command) ValueUpdatePolicy() string {
	if c.ValuesUpdate == "restart" {
		return "restart"
	}
	return "notify"
}

type Context struct {
	Document     *Document          `json:"-"`
	Bundle       string             `json:"-"`
	EnvFiles     []string           `json:"-"`
	Ports        map[string]Port    `json:"-"`
	Proxies      map[string]Proxy   `json:"-"`
	Requirements Requirements       `json:"-"`
	Profile      string             `json:"profile"`
	Source       string             `json:"source"`
	ConfigPath   string             `json:"config_path,omitempty"`
	Root         string             `json:"root,omitempty"`
	Commands     map[string]Command `json:"-"`
}

const ProfilePattern = `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`

// Command names are literal keys, with controls and leading option markers excluded.
const CommandPattern = `^[^-\x00-\x1F\x7F-\x9F][^\x00-\x1F\x7F-\x9F]{0,127}$`

var profilePattern = regexp.MustCompile(ProfilePattern)
var commandPattern = regexp.MustCompile(CommandPattern)

func ValidProfile(profile string) bool { return profilePattern.MatchString(profile) }
func ValidCommand(name string) bool    { return commandPattern.MatchString(name) }

// Resolve gives an explicit profile precedence over filesystem configuration.
// Otherwise it searches upward, including but never crossing a .git boundary.
func Resolve(start, explicitProfile string) (Context, *protocol.Error) {
	if explicitProfile != "" {
		if !ValidProfile(explicitProfile) {
			return Context{}, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, map[string]any{"field": "profile"})
		}
		return Context{Profile: explicitProfile, Source: "flag"}, nil
	}
	dir, err := location.Canonical(start)
	if err != nil {
		return Context{}, ioError(start, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return Context{}, ioError(dir, err)
	}
	if !info.IsDir() {
		return Context{}, protocol.NewError("invalid_argument", "Project search requires a directory.", 2, map[string]any{"path": dir})
	}
	for {
		path := filepath.Join(dir, Filename)
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			info, statErr := file.Stat()
			if statErr != nil || !info.Mode().IsRegular() {
				_ = file.Close()
				return Context{}, ioError(path, statErr)
			}
			data, readErr := io.ReadAll(io.LimitReader(file, configcontract.Limit+1))
			_ = file.Close()
			if readErr != nil {
				return Context{}, ioError(path, readErr)
			}
			return parse(data, path, dir)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Context{}, ioError(path, err)
		}
		_, err = os.Lstat(filepath.Join(dir, ".git"))
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Context{}, ioError(dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return Context{}, protocol.NewError("project_not_found", "No devtools.toml was found within this project boundary. Initialize a project or select a directory containing its configuration.", 3, map[string]any{"phase": "resolve", "reason": "not_found"})
}

func parse(data []byte, path, root string) (Context, *protocol.Error) {
	doc, e := ParseDocument(data, path, root)
	if e != nil {
		return Context{}, e
	}
	return doc.Context, nil
}

func parseTyped(data []byte, path, root string) (Context, *protocol.Error) {
	var config Config
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().EnableUnmarshalerInterface()
	if err := decoder.Decode(&config); err != nil {
		details := map[string]any{"path": path, "reason": "invalid_type"}
		causes := []any{}
		var missing *toml.StrictMissingError
		if errors.As(err, &missing) {
			details["reason"] = "unknown_field"
			for _, field := range missing.Errors {
				line, column := field.Position()
				causes = append(causes, map[string]any{"code": "invalid_config", "reason": "unknown_field", "target": map[string]any{"kind": "config", "name": Filename, "config_path": path}, "sources": []any{map[string]any{"kind": "config", "path": append([]string{}, field.Key()...), "line": line, "column": column}}, "facts": map[string]any{}})
			}
		}
		if len(causes) > 0 {
			details["causes"] = causes
		}
		return Context{}, protocol.NewError("invalid_config", "Invalid TOML type or unknown configuration fields. Run devtools config check.", 3, details)
	}
	if e := resolveRegistry(&config); e != nil {
		return Context{}, e
	}
	issues := []any{}
	add := func(reason string, source []string, message string) {
		issues = append(issues, registryIssue(reason, source, message))
	}
	if !ValidProfile(config.Profile) {
		add("invalid_value", []string{"profile"}, "A valid profile identifier is required.")
	}
	if config.Bundle != "" && !ValidProfile(config.Bundle) {
		add("invalid_value", []string{"bundle"}, "The project bundle identifier is invalid.")
	}
	for i, value := range config.EnvFiles {
		if value == "" || strings.ContainsRune(value, 0) {
			add("invalid_value", []string{"env_files", strconv.Itoa(i)}, "An env_files path is empty or contains NUL.")
		}
	}
	for _, name := range sortedKeys(config.Commands) {
		command := config.Commands[name]
		base := []string{"commands", name}
		field := func(key string) []string { return append(append([]string{}, base...), key) }
		if command.ValuesUpdate != "" && command.ValuesUpdate != "notify" && command.ValuesUpdate != "restart" {
			add("invalid_value", field("values_update"), "Use notify or restart for the value update policy.")
		}
		if command.Bundle != "" && !ValidProfile(command.Bundle) {
			add("invalid_value", field("bundle"), "The command bundle identifier is invalid.")
		}
		if !(redact.Policy{Mode: command.Output, Stdout: command.Stdout, Stderr: command.Stderr}).Valid() {
			add("invalid_value", base, "The command output policy is invalid.")
		}
		if command.Ready != nil && !command.Ready.Valid() {
			add("invalid_value", field("ready"), "The readiness command or timeout is invalid.")
		}
		if !validBindings(command, config.Ports) {
			add("invalid_reference", base, "The command port, serve, or bind declaration is invalid.")
		}
		if !command.Requirements.Valid() || !config.Requirements.Merge(command.Requirements).Valid() {
			add("invalid_requirements", field("requirements"), "Use valid tool specifications and distinct variable/secret keys.")
		}
		if !ValidCommand(name) {
			add("invalid_value", base, "The command name is invalid.")
		}
		if len(command.Exec) == 0 || command.Exec[0] == "" {
			add("invalid_value", field("exec"), "Provide a nonempty executable argv.")
		}
		if command.Env != "" && !ValidProfile(command.Env) {
			add("invalid_value", field("env"), "The command env identifier is invalid.")
		}
		for i, arg := range command.Exec {
			if strings.ContainsRune(arg, 0) {
				add("invalid_value", append(field("exec"), strconv.Itoa(i)), "Command arguments cannot contain NUL.")
			}
		}
	}
	if !config.Requirements.Valid() {
		add("invalid_requirements", []string{"requirements"}, "Use valid tool specifications and distinct variable/secret keys.")
	}
	for _, name := range sortedKeys(config.Ports) {
		if !ValidProfile(name) || !config.Ports[name].Valid() {
			add("invalid_value", []string{"ports", name}, "The port declaration is invalid.")
		}
	}
	for _, name := range sortedKeys(config.Proxies) {
		if !ValidProfile(name) || !config.Proxies[name].Valid(config.Ports) {
			add("invalid_value", []string{"proxies", name}, "The proxy declaration is invalid.")
		}
	}
	if e := registryError(issues); e != nil {
		return Context{}, e
	}
	return Context{Bundle: config.Bundle, Profile: config.Profile, Source: "file", ConfigPath: path, Root: root, Commands: config.Commands, Requirements: config.Requirements, Ports: config.Ports, Proxies: config.Proxies, EnvFiles: config.EnvFiles}, nil
}

func ioError(path string, cause error) *protocol.Error {
	failure := protocol.NewError("io_error", "Cannot read project configuration or directory. Inspect the reported I/O condition and selected --dir.", 1, map[string]any{"path": path, "phase": "resolve", "reason": "not_regular"})
	if cause == nil {
		return failure
	}
	return protocol.WithStorageDetails(failure, "project", "read_configuration", "configuration", cause)
}
