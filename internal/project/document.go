package project

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/pelletier/go-toml/v2"
)

type Preset struct {
	Tools []string `toml:"tools,omitempty"`
	Vars  []string `toml:"vars,omitempty"`
	Secs  []string `toml:"secs,omitempty"`
}
type Selection struct {
	Requires []string `toml:"requires,omitempty"`
	Tools    []string `toml:"tools,omitempty"`
	Vars     []string `toml:"vars,omitempty"`
	Secs     []string `toml:"secs,omitempty"`
}

type Document struct {
	Bytes   []byte
	Path    string
	Format  string
	Raw     map[string]any
	Syntax  *configcontract.Syntax
	Context Context
}

func ParseDocument(data []byte, path, root string) (*Document, *protocol.Error) {
	d := &Document{Bytes: append([]byte{}, data...), Path: path, Format: "unknown"}
	invalid := func(reason string, paths ...[]string) (*Document, *protocol.Error) {
		causes := []any{}
		for _, p := range paths {
			sources := []configcontract.Source{}
			if d.Syntax != nil {
				sources = append(sources, d.Syntax.Source(p))
			}
			causes = append(causes, map[string]any{"code": "invalid_config", "reason": reason, "target": map[string]any{"kind": "config", "name": Filename, "config_path": path}, "sources": sources, "facts": map[string]any{}})
		}
		return nil, protocol.NewError("invalid_config", "Project configuration is invalid. Run devtools config check for source diagnostics.", 3, map[string]any{"path": path, "format": d.Format, "reason": reason, "causes": causes, "target": map[string]any{"kind": "config", "name": Filename, "config_path": path}})
	}
	if len(data) > configcontract.Limit {
		return invalid("limit_exceeded")
	}
	if !utf8.Valid(data) {
		return invalid("invalid_encoding")
	}
	syntax, err := configcontract.ParseSyntax(data)
	if err != nil {
		_, e := invalid("syntax_error")
		var syntaxError *configcontract.SyntaxError
		if errors.As(err, &syntaxError) {
			e.Details["sources"] = []configcontract.Source{syntaxError.Source}
		}
		var de *toml.DecodeError
		if errors.As(err, &de) {
			line, col := de.Position()
			e.Details["sources"] = []any{map[string]any{"kind": "config", "path": []string{}, "line": line, "column": col}}
		}
		return nil, e
	}
	d.Syntax = syntax
	seen := map[string]bool{}
	for _, node := range syntax.Nodes {
		if node.Kind != "value" && node.Kind != "table" {
			continue
		}
		key := node.Kind + configcontract.PathKey(node.Path)
		if seen[key] {
			_, e := invalid("duplicate_field", node.Path)
			e.Details["sources"] = []configcontract.Source{syntax.At(node.Path, node.Start, node.End)}
			return nil, e
		}
		seen[key] = true
	}
	if err := toml.Unmarshal(data, &d.Raw); err != nil {
		_, e := invalid("syntax_error")
		var de *toml.DecodeError
		if errors.As(err, &de) {
			line, col := de.Position()
			e.Details["sources"] = []any{map[string]any{"kind": "config", "path": append([]string{}, de.Key()...), "line": line, "column": col}}
		}
		return nil, e
	}
	legacy, registry := false, false
	for _, n := range syntax.Nodes {
		if len(n.Path) > 0 && n.Path[0] == "requirements" || len(n.Path) > 2 && n.Path[0] == "commands" && n.Path[2] == "requirements" {
			legacy = true
		}
		if len(n.Path) > 0 && (n.Path[0] == "tools" || n.Path[0] == "presets" || n.Path[0] == "defaults") || len(n.Path) > 2 && n.Path[0] == "commands" && contains([]string{"requires", "tools", "vars", "secs", "tool_overrides"}, n.Path[2]) {
			registry = true
		}
	}
	if legacy && registry {
		return invalid("mixed_formats")
	}
	d.Format = "neutral"
	if legacy {
		d.Format = "legacy"
	}
	if registry {
		d.Format = "registry"
	}
	{
		var issues []configcontract.Issue
		if !legacy {
			issues = configcontract.Validate("Project", d.Raw)
		} else {
			// Legacy structure remains readable, but its tool fields follow the
			// same contract (including explicit empty ranges) as registry tools.
			validateTools := func(raw any, path []string) {
				req, _ := raw.(map[string]any)
				tools, _ := req["tools"].(map[string]any)
				for name, tool := range tools {
					for _, issue := range configcontract.Validate("Tool", tool) {
						issue.Path = append(append(append([]string{}, path...), "tools", name), issue.Path...)
						issues = append(issues, issue)
					}
				}
			}
			validateTools(d.Raw["requirements"], []string{"requirements"})
			commands, _ := d.Raw["commands"].(map[string]any)
			for name, raw := range commands {
				command, _ := raw.(map[string]any)
				validateTools(command["requirements"], []string{"commands", name, "requirements"})
			}
		}
		if len(issues) > 0 {
			sort.SliceStable(issues, func(i, j int) bool {
				a, b := d.Source(issues[i].Path), d.Source(issues[j].Path)
				if a.Offset != nil && b.Offset != nil && *a.Offset != *b.Offset {
					return *a.Offset < *b.Offset
				}
				if (a.Offset == nil) != (b.Offset == nil) {
					return a.Offset != nil
				}
				return configcontract.PathKey(a.Path)+issues[i].Reason < configcontract.PathKey(b.Path)+issues[j].Reason
			})
			paths := [][]string{}
			for _, i := range issues {
				paths = append(paths, i.Path)
			}
			_, e := invalid(issues[0].Reason, paths...)
			causes := e.Details["causes"].([]any)
			for index, issue := range issues {
				cause := causes[index].(map[string]any)
				cause["reason"] = issue.Reason
				cause["facts"] = map[string]any{"rule": issue.Rule}
			}
			return nil, e
		}
	}
	p, e := parseTyped(data, path, root)
	if e != nil {
		if causes, ok := e.Details["causes"].([]any); ok {
			for _, item := range causes {
				cause := item.(map[string]any)
				if sources, ok := cause["sources"].([]configcontract.Source); ok {
					for i, source := range sources {
						if source.Kind == "config" && source.Offset == nil {
							sources[i] = d.Source(source.Path)
						}
					}
					cause["sources"] = sources
				}
				if sources, ok := cause["sources"].([]any); ok {
					owned := []configcontract.Source{}
					for _, source := range sources {
						if object, ok := source.(map[string]any); ok {
							if sourcePath, ok := object["path"].([]string); ok {
								owned = append(owned, d.Source(sourcePath))
							}
						}
					}
					cause["sources"] = owned
				}
				if target, ok := cause["target"].(map[string]any); ok {
					target["config_path"] = path
				}
			}
			e.Details["path"] = path
			e.Details["format"] = d.Format
			sort.SliceStable(causes, func(i, j int) bool {
				a, b := causes[i].(map[string]any), causes[j].(map[string]any)
				first := func(cause map[string]any) configcontract.Source {
					if sources, ok := cause["sources"].([]configcontract.Source); ok && len(sources) > 0 {
						return sources[0]
					}
					return configcontract.Source{}
				}
				as, bs := first(a), first(b)
				if as.Offset != nil && bs.Offset != nil && *as.Offset != *bs.Offset {
					return *as.Offset < *bs.Offset
				}
				if (as.Offset == nil) != (bs.Offset == nil) {
					return as.Offset != nil
				}
				return configcontract.PathKey(as.Path)+a["code"].(string) < configcontract.PathKey(bs.Path)+b["code"].(string)
			})
			return nil, e
		}
		field, _ := e.Details["field"].(string)
		reason, _ := e.Details["reason"].(string)
		if reason == "" {
			reason = "invalid_declaration"
		}
		_, enriched := invalid(reason, []string{field})
		return nil, enriched
	}
	d.Context = p
	d.Context.Document = d
	return d, nil
}

func contains(items []string, s string) bool {
	for _, v := range items {
		if v == s {
			return true
		}
	}
	return false
}
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Equivalent compares execution semantics, not authoring/provenance differences.
func Equivalent(a, b Context) bool {
	normalize := func(r Requirements) Requirements {
		r = Requirements{}.Merge(r)
		for key, tool := range r.Tools {
			r.Tools[key] = tool.WithDefaults(key)
		}
		return r
	}
	return reflect.DeepEqual(Effective(a), Effective(b)) && reflect.DeepEqual(normalize(a.Requirements), normalize(b.Requirements))
}
func Effective(p Context) map[string]any {
	commands := map[string]any{}
	for name, c := range p.Commands {
		r := p.Requirements.Merge(c.Requirements)
		tools := map[string]Tool{}
		for k, t := range r.Tools {
			tools[k] = t.WithDefaults(k)
		}
		r.Tools = tools
		selection := c.Bundle
		if selection == "" {
			selection = p.Bundle
		}
		if selection == "" {
			selection = "all"
		}
		out := c.Output
		if out == "" {
			out = "auto"
		}
		ready := any(nil)
		if c.Ready != nil {
			ready = map[string]any{"exec": c.Ready.Exec, "timeout": c.Ready.Duration().String()}
		}
		commands[name] = map[string]any{"exec": c.Exec, "inject": c.Inject, "env": c.Env, "selection": selection, "output": out, "stdout": c.Stdout, "stderr": c.Stderr, "values_update": c.ValueUpdatePolicy(), "serve": append([]string{}, c.Serve...), "bind": c.Bind, "ready": ready, "requirements": r}
	}
	return map[string]any{"profile": p.Profile, "bundle": p.Bundle, "env_files": p.EnvFiles, "ports": p.Ports, "proxies": p.Proxies, "commands": commands}
}

// EncodeValue writes new TOML tokens. Existing source tokens are kept by the editor.
func EncodeValue(v any) string {
	switch v := v.(type) {
	case string:
		return configcontract.Quote(v)
	case bool:
		return fmt.Sprint(v)
	case int, int64, float64:
		return fmt.Sprint(v)
	case []any:
		parts := []string{}
		for _, item := range v {
			parts = append(parts, EncodeValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		parts := []string{}
		for _, key := range sortedKeys(v) {
			parts = append(parts, configcontract.Key(key)+" = "+EncodeValue(v[key]))
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		panic(fmt.Sprintf("unsupported TOML input %T", v))
	}
}

func (d *Document) Source(path []string) configcontract.Source { return d.Syntax.Source(path) }
func (d *Document) SameBytes(data []byte) bool                 { return bytes.Equal(d.Bytes, data) }
