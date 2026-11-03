package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/collectionquery"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type collectionScope struct {
	Kind        string `json:"kind"`
	Profile     string `json:"profile,omitempty"`
	Directory   string `json:"directory,omitempty"`
	Instance    string `json:"instance_id,omitempty"`
	AllProfiles bool   `json:"all_profiles,omitempty"`
	Global      bool   `json:"global,omitempty"`
	RowsScoped  bool   `json:"-"`
}

func scopeRequired() *protocol.Error {
	return protocol.NewError("scope_required", "Select a project directory, a profile, an instance, or explicit --all-profiles for this collection.", 3, map[string]any{"effects": "none", "remedies": []protocol.Remedy{
		{Relation: protocol.SameStoreAction, Argv: []string{"devtools", "profile", "list"}, RequiredInputs: []string{}, Effect: "read", Message: "Discover available profiles."},
		{Relation: protocol.SameStoreAction, Argv: []string{"devtools", "instance", "list", "--all-profiles"}, RequiredInputs: []string{}, Effect: "read", Message: "Discover execution locations before selecting a scope."},
	}})
}

func (a *App) resolveCollectionScope(_ context.Context, c Command, r Request) (collectionScope, *protocol.Error) {
	s := collectionScope{Kind: c.Collection.Scope, Profile: r.Options["profile"], AllProfiles: r.Options["all-profiles"] == "true", Global: r.Options["global"] == "true", RowsScoped: c.Collection.Scope == "project" || c.Name == "cleanup archives"}
	if s.AllProfiles && s.Profile != "" {
		return s, argumentError("Choose --all-profiles or --profile.", "profile")
	}
	if s.Global {
		if s.Profile != "" || r.Options["dir"] != "" || r.Options["instance"] != "" || s.AllProfiles {
			return s, argumentError("--global excludes project scope selectors.", "global")
		}
		s.Kind = "global"
		return s, nil
	}
	if s.Kind == "global" {
		return s, nil
	}
	selector, directory := r.Options["instance"], r.Options["dir"]
	implicit := s.Profile == "" && !s.AllProfiles && selector == "" && directory == ""
	if directory != "" || implicit || s.Kind == "directory" {
		if directory == "" {
			directory = "."
		}
		canonical, err := location.Canonical(directory)
		if err != nil {
			return s, argumentError("Cannot resolve the selected directory.", "dir")
		}
		p, e := project.Resolve(directory, "")
		if e == nil {
			if s.Profile != "" && s.Profile != p.Profile {
				return s, argumentError("The selected directory belongs to another profile.", "profile")
			}
			s.Profile, s.Directory = p.Profile, p.Root
		} else if e.Code == "project_not_found" && !implicit && s.Kind == "project" {
			s.Directory = canonical // An explicitly selected retired location is still queryable.
		} else if e.Code == "project_not_found" {
			return s, scopeRequired()
		} else {
			return s, e
		}
	}
	if selector != "" || c.Name == "expose list" && s.Directory != "" {
		store, e := a.portStore()
		if e != nil {
			return s, e
		}
		state, e := store.Read()
		if e != nil {
			return s, e
		}
		matches := 0
		for _, instance := range state.Instances {
			if s.Profile != "" && instance.Profile != s.Profile {
				continue
			}
			if selector != "" && selector != "id:"+instance.ID && (instance.Alias == nil || selector != *instance.Alias) {
				continue
			}
			if s.Directory != "" && !location.Same(s.Directory, instance.Directory) {
				continue
			}
			matches++
			s.Instance = instance.ID
			if selector != "" {
				s.Profile = instance.Profile
			}
		}
		if selector != "" {
			if matches == 0 {
				return s, argumentError("The instance is missing or conflicts with the selected profile/directory.", "instance")
			}
			if matches > 1 {
				return s, scopeRequired()
			}
			s.Kind = "instance"
		}
	}
	return s, nil
}

func (s collectionScope) matches(row any) bool {
	profile := collectionquery.String(row, "profile")
	if profile == "" {
		profile = collectionquery.String(row, "route_ref.profile")
	}
	directory := collectionquery.String(row, "directory")
	instance := collectionquery.String(row, "instance_id")
	if instance == "" {
		instance = collectionquery.String(row, "route_ref.instance_id")
	}
	if s.Profile != "" && (profile != "" && profile != s.Profile || profile == "" && s.RowsScoped) {
		return false
	}
	if s.Instance != "" && instance != s.Instance {
		return false
	}
	if s.Directory != "" {
		if directory != "" && !location.Same(directory, s.Directory) {
			return false
		}
		if directory == "" && s.RowsScoped && (s.Instance == "" || instance != s.Instance) {
			return false
		}
	}
	if s.Global {
		global := collectionquery.String(row, "scope") == "global" || collectionquery.String(row, "route_ref.scope") == "global"
		if !global {
			return false
		}
	}
	return true
}
