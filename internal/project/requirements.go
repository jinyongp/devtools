package project

import (
	"regexp"
	"sort"
	"strings"
)

// Requirements describe presence and exact-version prerequisites for a project.
type Requirements struct {
	Tools map[string]Tool `toml:"tools,omitempty" json:"tools"`
	Vars  []string        `toml:"vars,omitempty" json:"vars"`
	Secs  []string        `toml:"secs,omitempty" json:"secs"`
}
type Tool struct {
	Executable  string   `toml:"executable,omitempty" json:"executable"`
	Version     string   `toml:"version,omitempty" json:"version"`
	VersionArgs []string `toml:"version_args,omitempty" json:"version_args"`
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+(?:\.[0-9]+)?(?:-[0-9A-Za-z.-]+)?$`)
var requiredKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (r Requirements) Empty() bool { return len(r.Tools)+len(r.Vars)+len(r.Secs) == 0 }
func (r Requirements) Valid() bool {
	for name, t := range r.Tools {
		if !ValidProfile(name) || strings.ContainsRune(t.Executable, 0) || t.Version != "" && !versionPattern.MatchString(t.Version) || len(t.Version) > 128 || len(t.VersionArgs) > 32 || t.Version == "" && len(t.VersionArgs) > 0 {
			return false
		}
		for _, arg := range t.VersionArgs {
			if strings.ContainsRune(arg, 0) || len(arg) > 4096 {
				return false
			}
		}
	}
	vars := map[string]bool{}
	for _, k := range r.Vars {
		if !requiredKeyPattern.MatchString(k) {
			return false
		}
		vars[k] = true
	}
	for _, k := range r.Secs {
		if !requiredKeyPattern.MatchString(k) || vars[k] {
			return false
		}
	}
	return true
}

// Merge lets command-specific tools override project tools; key sets accumulate.
func (r Requirements) Merge(local Requirements) Requirements {
	out := Requirements{Tools: map[string]Tool{}}
	for k, v := range r.Tools {
		out.Tools[k] = v
	}
	for k, v := range local.Tools {
		out.Tools[k] = v
	}
	merge := func(a, b []string) []string {
		set := map[string]bool{}
		for _, k := range append(append([]string{}, a...), b...) {
			set[k] = true
		}
		out := []string{}
		for k := range set {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	out.Vars = merge(r.Vars, local.Vars)
	out.Secs = merge(r.Secs, local.Secs)
	return out
}
