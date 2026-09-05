// Package values manages profile values and their common/environment layers.
package values

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

const KeyPattern = `^[A-Za-z_][A-Za-z0-9_]*$`

var keyPattern = regexp.MustCompile(KeyPattern)

type Kind string

const (
	Variable Kind = "variable"
	Secret   Kind = "secret"
)

type entry struct {
	Kind   Kind              `json:"kind"`
	Common *string           `json:"common,omitempty"`
	Envs   map[string]string `json:"envs"`
}

// State is a private-storage snapshot. CLI responses use Metadata instead.
type State struct {
	Version int               `json:"version"`
	Profile string            `json:"profile"`
	Envs    map[string]bool   `json:"envs"`
	Keys    map[string]*entry `json:"keys"`
}

type Metadata struct {
	Key       string `json:"key"`
	Kind      Kind   `json:"kind"`
	Source    string `json:"source"`
	Overrides bool   `json:"overrides"`
}

func newState(profile string) *State {
	return &State{Version: 1, Profile: profile, Envs: map[string]bool{}, Keys: map[string]*entry{}}
}

func failure(code, message string) *protocol.Error { return protocol.NewError(code, message, 3, nil) }

func (s *State) CheckEnv(env string) *protocol.Error {
	if env != "" && !s.Envs[env] {
		return failure("env_not_found", "Create the selected env before using it.")
	}
	return nil
}

func (s *State) CreateEnv(env string) (bool, *protocol.Error) {
	if !project.ValidProfile(env) {
		return false, protocol.NewError("invalid_argument", "Invalid env name.", 2, nil)
	}
	if s.Envs[env] {
		return false, nil
	}
	s.Envs[env] = true
	return true, nil
}

func (s *State) RemoveEnv(env string) (bool, *protocol.Error) {
	if err := s.CheckEnv(env); err != nil {
		return false, err
	}
	for _, value := range s.Keys {
		if _, exists := value.Envs[env]; exists {
			return false, failure("env_not_empty", "Remove this env's variable and secret values before deleting it.")
		}
	}
	delete(s.Envs, env)
	return true, nil
}

func (s *State) EnvNames() []string {
	names := make([]string, 0, len(s.Envs))
	for name := range s.Envs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validValue(value string) bool { return utf8.ValidString(value) && !strings.ContainsRune(value, 0) }

func (s *State) Set(kind Kind, key, env, value string) (bool, *protocol.Error) {
	if !keyPattern.MatchString(key) || !validValue(value) || (kind != Variable && kind != Secret) {
		return false, protocol.NewError("invalid_argument", "Use a valid environment key and UTF-8 value without NUL bytes.", 2, nil)
	}
	if err := s.CheckEnv(env); err != nil {
		return false, err
	}
	e, found := s.Keys[key]
	if found && e.Kind != kind {
		return false, failure("kind_conflict", "The key has a different kind in this profile.")
	}
	if !found {
		e = &entry{Kind: kind, Envs: map[string]string{}}
		s.Keys[key] = e
	}
	if env == "" {
		if e.Common != nil && *e.Common == value {
			return false, nil
		}
		e.Common = &value
	} else {
		if old, ok := e.Envs[env]; ok && old == value {
			return false, nil
		}
		e.Envs[env] = value
	}
	return true, nil
}

func (s *State) Unset(kind Kind, key, env string) (bool, *protocol.Error) {
	if err := s.CheckEnv(env); err != nil {
		return false, err
	}
	e, exists := s.Keys[key]
	if !exists {
		return false, nil
	}
	if e.Kind != kind {
		return false, failure("kind_conflict", "The key has a different kind in this profile.")
	}
	if env == "" {
		if e.Common == nil {
			return false, nil
		}
		e.Common = nil
	} else {
		if _, exists := e.Envs[env]; !exists {
			return false, nil
		}
		delete(e.Envs, env)
	}
	return true, nil
}

func effective(e *entry, env string) (string, string, bool) {
	if env != "" {
		if value, ok := e.Envs[env]; ok {
			return value, "env", true
		}
	}
	if e.Common != nil {
		return *e.Common, "common", true
	}
	return "", "", false
}

func (s *State) List(kind Kind, env string) ([]Metadata, *protocol.Error) {
	if err := s.CheckEnv(env); err != nil {
		return nil, err
	}
	items := []Metadata{}
	for key, e := range s.Keys {
		_, source, exists := effective(e, env)
		if e.Kind == kind && exists {
			items = append(items, Metadata{Key: key, Kind: kind, Source: source, Overrides: source == "env" && e.Common != nil})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items, nil
}

func (s *State) GetVariable(key, env string) (string, Metadata, *protocol.Error) {
	if err := s.CheckEnv(env); err != nil {
		return "", Metadata{}, err
	}
	e, ok := s.Keys[key]
	if !ok {
		return "", Metadata{}, failure("key_not_found", "The variable has no value in the selected scope.")
	}
	if e.Kind != Variable {
		return "", Metadata{}, failure("kind_conflict", "The key has a different kind in this profile.")
	}
	value, source, ok := effective(e, env)
	if !ok {
		return "", Metadata{}, failure("key_not_found", "The variable has no value in the selected scope.")
	}
	return value, Metadata{Key: key, Kind: e.Kind, Source: source, Overrides: source == "env" && e.Common != nil}, nil
}

// Environment returns values only to the process-execution boundary.
func (s *State) Environment(env string) (map[string]string, *protocol.Error) {
	if err := s.CheckEnv(env); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for key, e := range s.Keys {
		if value, _, ok := effective(e, env); ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *State) valid(profile string) bool {
	if s.Version != 1 || s.Profile != profile || s.Envs == nil || s.Keys == nil {
		return false
	}
	for env, enabled := range s.Envs {
		if !enabled || !project.ValidProfile(env) {
			return false
		}
	}
	for key, e := range s.Keys {
		if !keyPattern.MatchString(key) || e == nil || (e.Kind != Variable && e.Kind != Secret) || e.Envs == nil {
			return false
		}
		if e.Common != nil && !validValue(*e.Common) {
			return false
		}
		for env, value := range e.Envs {
			if !s.Envs[env] || !validValue(value) {
				return false
			}
		}
	}
	return true
}
