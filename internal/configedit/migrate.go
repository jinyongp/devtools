package configedit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func migration(doc *project.Document) (map[string]any, *protocol.Error) {
	if doc.Format != "legacy" {
		return clone(doc.Raw), nil
	}
	out := clone(doc.Raw)
	delete(out, "requirements")
	commands := object(out, "commands")
	registry := map[string]any{}
	base := doc.Context.Requirements
	legacyRoot, _ := doc.Raw["requirements"].(map[string]any)
	globalTools, _ := legacyRoot["tools"].(map[string]any)
	for name, tool := range globalTools {
		registry[name] = migrationTool(tool)
	}
	for _, name := range keys(commands) {
		raw := commands[name].(map[string]any)
		req, _ := raw["requirements"].(map[string]any)
		tools, _ := req["tools"].(map[string]any)
		for _, tool := range keys(tools) {
			if _, ok := registry[tool]; !ok {
				registry[tool] = migrationTool(tools[tool])
			}
		}
	}
	if len(registry) > 0 {
		out["tools"] = registry
	}
	defaults := selection(base, nil)
	if len(defaults) > 0 {
		out["defaults"] = defaults
	}
	selections := map[string]map[string]any{}
	groups := map[string][]string{}
	for _, name := range keys(commands) {
		raw := clone(commands[name].(map[string]any))
		delete(raw, "requirements")
		effective := base.Merge(doc.Context.Commands[name].Requirements)
		selected := selection(effective, &base)
		overrides := map[string]any{}
		for key, tool := range effective.Tools {
			var original project.Tool
			b, _ := json.Marshal(registry[key])
			_ = json.Unmarshal(b, &original)
			if !reflect.DeepEqual(tool.WithDefaults(key), original.WithDefaults(key)) {
				if legacy, ok := commands[name].(map[string]any)["requirements"].(map[string]any); ok {
					if tools, ok := legacy["tools"].(map[string]any); ok {
						if value, ok := tools[key]; ok {
							overrides[key] = migrationTool(value)
							continue
						}
					}
				}
				b, _ := json.Marshal(tool)
				var value map[string]any
				_ = json.Unmarshal(b, &value)
				if value["executable"] == "" {
					delete(value, "executable")
				}
				if value["version"] == "" {
					delete(value, "version")
				}
				if args, ok := value["version_args"].([]any); !ok || len(args) == 0 {
					delete(value, "version_args")
				}
				overrides[key] = value
			}
		}
		if len(overrides) > 0 {
			raw["tool_overrides"] = overrides
		}
		selections[name] = selected
		if len(selected) > 0 {
			b, _ := json.Marshal(selected)
			groups[string(b)] = append(groups[string(b)], name)
		}
		commands[name] = raw
	}
	presets := map[string]any{}
	for _, canonical := range sortedGroupKeys(groups) {
		names := groups[canonical]
		if len(names) < 2 {
			continue
		}
		sum := sha256.Sum256([]byte(canonical))
		hexsum := hex.EncodeToString(sum[:])
		name := ""
		for n := 12; n <= 64; n += 2 {
			candidate := "shared-" + hexsum[:n]
			if _, exists := presets[candidate]; !exists {
				name = candidate
				break
			}
		}
		if name == "" {
			return nil, failure("config_migration_conflict", "preset_name_collision", nil)
		}
		presets[name] = selections[names[0]]
		for _, command := range names {
			commands[command].(map[string]any)["requires"] = []any{name}
			delete(selections, command)
		}
	}
	if len(presets) > 0 {
		out["presets"] = presets
	}
	for name, selected := range selections {
		for k, v := range selected {
			commands[name].(map[string]any)[k] = v
		}
	}
	if _, existed := doc.Raw["commands"]; !existed && len(commands) == 0 {
		delete(out, "commands")
	}
	return out, nil
}

func migrationTool(value any) any {
	m := clone(value.(map[string]any))
	for _, key := range []string{"executable", "version"} {
		if m[key] == "" {
			delete(m, key)
		}
	}
	return m
}
func sortedGroupKeys(m map[string][]string) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func selection(r project.Requirements, base *project.Requirements) map[string]any {
	tools := []string{}
	for name := range r.Tools {
		if base == nil {
			tools = append(tools, name)
		} else if _, exists := base.Tools[name]; !exists {
			tools = append(tools, name)
		}
	}
	sort.Strings(tools)
	remaining := func(items, common []string) []any {
		seen := map[string]bool{}
		for _, k := range common {
			seen[k] = true
		}
		out := []any{}
		sort.Strings(items)
		for _, k := range items {
			if !seen[k] {
				out = append(out, k)
				seen[k] = true
			}
		}
		return out
	}
	vars, secs := []string{}, []string{}
	if base != nil {
		vars = base.Vars
		secs = base.Secs
	}
	out := map[string]any{}
	if len(tools) > 0 {
		a := []any{}
		for _, k := range tools {
			a = append(a, k)
		}
		out["tools"] = a
	}
	if a := remaining(append([]string{}, r.Vars...), vars); len(a) > 0 {
		out["vars"] = a
	}
	if a := remaining(append([]string{}, r.Secs...), secs); len(a) > 0 {
		out["secs"] = a
	}
	return out
}
