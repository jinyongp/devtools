package project

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/versionrange"
)

func registryIssue(reason string, path []string, message string) map[string]any {
	return map[string]any{"code": "invalid_config", "reason": reason, "message": message, "target": map[string]any{"kind": "config", "name": Filename}, "sources": []configcontract.Source{{Kind: "config", Path: append([]string{}, path...)}}, "facts": map[string]any{}}
}
func registryError(issues []any) *protocol.Error {
	if len(issues) == 0 {
		return nil
	}
	message := "Project declarations are invalid. " + issues[0].(map[string]any)["message"].(string)
	return protocol.NewError("invalid_config", message, 3, map[string]any{"reason": "invalid_requirements", "causes": issues})
}
func resolveRegistry(c *Config) *protocol.Error {
	registry := c.Tools != nil || c.Presets != nil || !reflect.DeepEqual(c.Defaults, Selection{})
	for _, cmd := range c.Commands {
		registry = registry || !reflect.DeepEqual(cmd.Selection, Selection{}) || cmd.ToolOverrides != nil
	}
	if !registry {
		return nil
	}
	issues := []any{}
	appendIssues := func(e *protocol.Error) {
		if e != nil {
			if causes, ok := e.Details["causes"].([]any); ok {
				issues = append(issues, causes...)
			}
		}
	}
	for _, name := range sortedKeys(c.Presets) {
		p := c.Presets[name]
		_, e := selectRequirements(c, Selection{Tools: p.Tools, Vars: p.Vars, Secs: p.Secs}, []string{"presets", name})
		appendIssues(e)
	}
	base, e := selectRequirements(c, c.Defaults, []string{"defaults"})
	appendIssues(e)
	c.Requirements = base
	for _, name := range sortedKeys(c.Commands) {
		cmd := c.Commands[name]
		path := []string{"commands", name}
		local, e := selectRequirements(c, cmd.Selection, path)
		appendIssues(e)
		effective := base.Merge(local)
		for _, key := range sortedKeys(cmd.ToolOverrides) {
			tool := cmd.ToolOverrides[key]
			if _, ok := effective.Tools[key]; !ok {
				issues = append(issues, registryIssue("unselected_override", append(append([]string{}, path...), "tool_overrides", key), fmt.Sprintf("Tool override %q is not selected by this command or defaults.", key)))
			}
			effective.Tools[key] = tool
		}
		variables := map[string]bool{}
		for _, key := range effective.Vars {
			variables[key] = true
		}
		for _, key := range effective.Secs {
			if variables[key] {
				issues = append(issues, registryIssue("kind_conflict", path, fmt.Sprintf("Required key %q is selected as both a variable and a secret.", key)))
			}
		}
		probes := map[string][]string{}
		executables := map[string]string{}
		for _, key := range sortedKeys(effective.Tools) {
			tool := effective.Tools[key].WithDefaults(key)
			if tool.Version == "" {
				continue
			}
			identity := tool.Executable + "\x00" + strings.Join(tool.VersionArgs, "\x00")
			probes[identity] = append(probes[identity], tool.Version)
			executables[identity] = tool.Executable
		}
		for _, identity := range sortedKeys(probes) {
			if !versionrange.Intersects(probes[identity]...) {
				issue := registryIssue("version_conflict", path, fmt.Sprintf("Executable %q has version requirements with no common version for the same probe.", executables[identity]))
				issue["facts"] = map[string]any{"executable": executables[identity], "allowed": probes[identity]}
				issues = append(issues, issue)
			}
		}
		cmd.Requirements = effective
		c.Commands[name] = cmd
	}
	unique := []any{}
	seen := map[string]bool{}
	for _, item := range issues {
		issue := item.(map[string]any)
		source := issue["sources"].([]configcontract.Source)[0]
		key := issue["reason"].(string) + configcontract.PathKey(source.Path) + issue["message"].(string)
		if !seen[key] {
			unique = append(unique, item)
			seen[key] = true
		}
	}
	return registryError(unique)
}
func selectRequirements(c *Config, s Selection, path []string) (Requirements, *protocol.Error) {
	r := Requirements{Tools: map[string]Tool{}, Vars: append([]string{}, s.Vars...), Secs: append([]string{}, s.Secs...)}
	issues := []any{}
	addTool := func(name string, source []string) {
		if tool, ok := c.Tools[name]; ok {
			r.Tools[name] = tool
		} else {
			issue := registryIssue("unknown_reference", source, fmt.Sprintf("Tool %q is not registered in [tools].", name))
			issue["target"] = map[string]any{"kind": "tool", "name": name}
			issues = append(issues, issue)
		}
	}
	for i, name := range s.Tools {
		addTool(name, append(append([]string{}, path...), "tools", strconv.Itoa(i)))
	}
	for i, name := range s.Requires {
		p, ok := c.Presets[name]
		if !ok {
			issue := registryIssue("unknown_reference", append(append([]string{}, path...), "requires", strconv.Itoa(i)), fmt.Sprintf("Preset %q is not declared in [presets].", name))
			issue["target"] = map[string]any{"kind": "preset", "name": name}
			issues = append(issues, issue)
			continue
		}
		for j, tool := range p.Tools {
			addTool(tool, []string{"presets", name, "tools", strconv.Itoa(j)})
		}
		r.Vars = append(r.Vars, p.Vars...)
		r.Secs = append(r.Secs, p.Secs...)
	}
	r = Requirements{}.Merge(r)
	variables := map[string]bool{}
	for _, key := range r.Vars {
		variables[key] = true
	}
	for _, key := range r.Secs {
		if variables[key] {
			issues = append(issues, registryIssue("kind_conflict", path, fmt.Sprintf("Required key %q is selected as both a variable and a secret.", key)))
		}
	}
	return r, registryError(issues)
}
