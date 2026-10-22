package project

import (
	"sort"
	"strconv"

	"github.com/jinyongp/devtools/internal/configcontract"
)

type Provenance struct {
	Path    []string                `json:"path"`
	Sources []configcontract.Source `json:"sources"`
}

func (d *Document) CommandSource(name string, path []string) configcontract.Source {
	if len(path) == 2 && path[0] == "tools" {
		key := path[1]
		candidates := [][]string{{"commands", name, "tool_overrides", key}, {"tools", key}}
		if d.Format == "legacy" {
			candidates = [][]string{{"commands", name, "requirements", "tools", key}, {"requirements", "tools", key}}
		}
		for _, candidate := range candidates {
			if _, ok := declaredAt(d.Raw, candidate); ok {
				return d.Source(candidate)
			}
		}
	}
	return d.Source(path)
}

func declaredAt(raw map[string]any, path []string) (any, bool) {
	var value any = raw
	for _, key := range path {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// ProvenanceFor records the declarations that select each effective condition,
// and the field/default that supplies its final tool specification.
func (d *Document) ProvenanceFor(name string, overrides map[string]configcontract.Source) []Provenance {
	out := []Provenance{}
	defaultSource := configcontract.Source{Kind: "default", Path: []string{}}
	add := func(path []string, sources ...configcontract.Source) {
		if len(sources) == 0 {
			sources = append(sources, defaultSource)
		}
		out = append(out, Provenance{Path: path, Sources: sources})
	}
	commandPath := []string{"commands", name}
	for _, field := range []string{"exec", "inject", "env", "bundle", "output", "stdout", "stderr", "values_update", "serve", "bind"} {
		path := append(append([]string{}, commandPath...), field)
		sources := []configcontract.Source{}
		if _, ok := declaredAt(d.Raw, path); ok {
			sources = append(sources, d.Source(path))
		} else if field == "bundle" {
			if _, ok := d.Raw["bundle"]; ok {
				sources = append(sources, d.Source([]string{"bundle"}))
			}
		}
		if source, ok := overrides[field]; ok {
			sources = append(sources, source)
		}
		effective := []string{field}
		if field == "bundle" {
			effective = []string{"selection"}
		}
		if field == "output" {
			effective = []string{"output", "mode"}
		}
		if field == "stdout" || field == "stderr" {
			effective = []string{"output", field}
		}
		add(effective, sources...)
	}
	command := d.Context.Commands[name]
	if command.Ready == nil {
		add([]string{"ready"})
	} else {
		for _, field := range []string{"exec", "timeout"} {
			path := append(append([]string{}, commandPath...), "ready", field)
			if _, ok := declaredAt(d.Raw, path); ok {
				add([]string{"ready", field}, d.Source(path))
			} else {
				add([]string{"ready", field})
			}
		}
	}
	selectors := func(kind, key string) []configcontract.Source {
		sources := []configcontract.Source{}
		for _, root := range [][]string{{"defaults"}, commandPath} {
			if d.Format == "legacy" {
				root = append(append([]string{}, root...), "requirements")
				if root[0] == "defaults" {
					root = []string{"requirements"}
				}
			}
			path := append(append([]string{}, root...), kind)
			if value, ok := declaredAt(d.Raw, path); ok {
				if m, ok := value.(map[string]any); ok {
					if _, ok := m[key]; ok {
						sources = append(sources, d.Source(append(path, key)))
					}
				}
				if a, ok := value.([]any); ok {
					for i, v := range a {
						if v == key {
							sources = append(sources, d.Source(append(path, strconv.Itoa(i))))
						}
					}
				}
			}
			if d.Format == "legacy" {
				continue
			}
			if selected, ok := declaredAt(d.Raw, append(append([]string{}, root...), "requires")); ok {
				if a, ok := selected.([]any); ok {
					names := []string{}
					for _, v := range a {
						names = append(names, v.(string))
					}
					sort.Strings(names)
					for _, preset := range names {
						path := []string{"presets", preset, kind}
						if value, ok := declaredAt(d.Raw, path); ok {
							for i, v := range value.([]any) {
								if v == key {
									sources = append(sources, d.Source(append(path, strconv.Itoa(i))))
								}
							}
						}
					}
				}
			}
		}
		return sources
	}
	r := d.Context.Requirements.Merge(command.Requirements)
	for _, tool := range sortedKeys(r.Tools) {
		selected := selectors("tools", tool)
		base := []string{"tools", tool}
		if d.Format == "legacy" {
			base = []string{"requirements", "tools", tool}
			local := []string{"commands", name, "requirements", "tools", tool}
			if _, ok := declaredAt(d.Raw, local); ok {
				base = local
			}
		} else {
			local := []string{"commands", name, "tool_overrides", tool}
			if _, ok := declaredAt(d.Raw, local); ok {
				base = local
			}
		}
		for _, field := range []string{"executable", "version", "version_args"} {
			sources := append([]configcontract.Source{}, selected...)
			path := append(append([]string{}, base...), field)
			if _, ok := declaredAt(d.Raw, path); ok {
				sources = append(sources, d.Source(path))
			} else {
				sources = append(sources, defaultSource)
			}
			add([]string{"requirements", "tools", tool, field}, sources...)
		}
	}
	for _, kind := range []string{"vars", "secs"} {
		keys := r.Vars
		if kind == "secs" {
			keys = r.Secs
		}
		for i, key := range keys {
			add([]string{"requirements", kind, strconv.Itoa(i)}, selectors(kind, key)...)
		}
	}
	return out
}
