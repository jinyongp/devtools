package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/configedit"
	"github.com/jinyongp/devtools/internal/doctor"
	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/projectfile"
	"github.com/jinyongp/devtools/internal/protocol"
)

func configOptions() []Option {
	return []Option{{Name: "dir", Default: ".", MinLength: 1, Description: "Search for devtools.toml within this project boundary."}}
}
func configWriteOptions(required bool, body bool) []Option {
	opts := append(configOptions(), Option{Name: "revision", Required: required, Pattern: `^[0-9a-f]{64}$`, Description: "Opaque revision returned by config inspect; detects changes before publication."}, Option{Name: "dry-run", Boolean: true, Description: "Validate and preview the same candidate without publishing it."})
	if body {
		opts = append(opts, Option{Name: "file", MinLength: 1, Description: "Read a strict JSON body from this regular file, relative to process cwd."}, Option{Name: "stdin", Boolean: true, Description: "Read a strict JSON body from redirected stdin."})
	}
	return opts
}
func (a *App) registerConfig() {
	a.commands = append(a.commands, Command{Name: "config schema", Description: "Return the authoritative configuration/edit/diagnostic JSON Schema without project lookup.", Output: configcontract.Definition("SchemaResult"), Run: func(context.Context, IO, Request) (any, *protocol.Error) {
		return map[string]any{"protocol_version": protocol.ProtocolVersion, "schema": configcontract.Schema()}, nil
	}})
	for _, action := range []string{"inspect", "check", "fmt", "edit", "migrate"} {
		opts := configOptions()
		output := "InspectResult"
		body := ""
		switch action {
		case "check":
			output = "ValidationReport"
		case "fmt":
			opts = configWriteOptions(false, false)
			opts = append(opts, Option{Name: "check", Boolean: true, Description: "Exit 3 with the preview report if formatting is required."})
			output = "EditResult"
		case "edit":
			opts = configWriteOptions(true, true)
			body = "Edit"
			output = "EditResult"
		case "migrate":
			opts = configWriteOptions(false, false)
			opts = append(opts, Option{Name: "apply", Pattern: `^[0-9a-f]{64}$`, Description: "Publish the candidate approved by the migration preview digest; requires --revision."})
			output = "MigrationResult"
		}
		c := Command{Name: "config " + action, Description: map[string]string{"inspect": "Read the declared configuration, source format and revision without reading stored values.", "check": "Validate syntax, declarations and static references; tool presence is checked by doctor.", "fmt": "Format declarations while preserving comments, values and order; never migrate requirements.", "edit": "Apply one atomic typed JSON operation batch; inspect first and supply its revision.", "migrate": "Preview a semantics-preserving legacy migration; apply only with its digest and revision."}[action], Options: opts, Output: configcontract.Definition(output), Run: a.configAction(action)}
		if body != "" {
			c.BodySchema = configcontract.Definition(body)
		}
		configMetadata(&c)
		a.commands = append(a.commands, c)
	}
	for _, kind := range []string{"tool", "preset", "command"} {
		for _, action := range []string{"add", "update", "remove", "rename"} {
			if action == "rename" && kind != "command" {
				continue
			}
			name := "config " + kind + " " + action
			if kind == "command" {
				name = "command " + action
			}
			body := action == "add" || action == "update"
			required := action != "add"
			opts := configWriteOptions(required, body)
			pattern := project.ProfilePattern
			if kind == "command" {
				pattern = project.CommandPattern
			}
			description := map[string]string{
				"add":    "Create a new " + kind + " declaration; existing names are rejected. Preview with --dry-run.",
				"update": "Patch the " + kind + " at its inspected revision; omitted fields are retained.",
				"remove": "Remove the " + kind + " at its inspected revision; remaining references prevent publication.",
				"rename": "Rename a literal command key at its inspected revision while preserving its definition.",
			}[action]
			c := Command{Name: name, Description: description, Options: opts, Arguments: []Argument{{Name: "name", Required: true, Pattern: pattern}}, Output: configcontract.Definition("EditResult"), Run: a.configResource(kind, action)}
			if action == "rename" {
				c.Arguments = append(c.Arguments, Argument{Name: "new_name", Required: true, Pattern: pattern})
			}
			if body {
				def := strings.ToUpper(kind[:1]) + kind[1:]
				if action == "update" {
					def += "Patch"
				}
				c.BodySchema = configcontract.Definition(def)
			}
			if kind == "command" && action == "add" {
				c.ChildArgs = true
			}
			configMetadata(&c)
			a.commands = append(a.commands, c)
		}
	}
	a.commands = append(a.commands, Command{Name: "command explain", Description: "Resolve one command and show declaration/default provenance without reading stored values or running probes.", Arguments: []Argument{{Name: "name", Required: true, Pattern: project.CommandPattern}}, Options: append(configOptions(), Option{Name: "profile", Pattern: project.ProfilePattern, Description: "Override the selected profile without bypassing configuration lookup."}, Option{Name: "env", Pattern: `^$|` + project.ProfilePattern, Description: "Override env, including the empty common layer; enables injection."}, Option{Name: "bundle", Pattern: project.ProfilePattern, Description: "Override value selection (all, none or a named bundle)."}), Output: configcontract.Definition("ExplainResult"), Run: a.explainCommand})
}

func readConfigBody(ctx context.Context, streams IO, r Request, definition string) (map[string]any, *protocol.Error) {
	file, stdin := r.Options["file"], r.Options["stdin"] == "true"
	if (file != "") == stdin {
		return nil, argumentError("Select exactly one JSON input: --file FILE or --stdin.", "file")
	}
	body, e := taskInput(ctx, streams, r)
	if e != nil {
		return nil, e
	}
	data := []byte(body)
	v, issue := configcontract.Decode(data)
	if issue != nil {
		source := map[string]any{"kind": "json", "path": append([]string{}, issue.Path...)}
		if issue.Offset >= 0 {
			source["offset"] = issue.Offset
		}
		return nil, protocol.NewError("invalid_argument", "Invalid strict JSON input; inspect the indicated field and rule.", 2, map[string]any{"reason": issue.Reason, "sources": []any{source}, "facts": map[string]any{"rule": issue.Rule}})
	}
	if issues := configcontract.Validate(definition, v); len(issues) > 0 {
		return nil, protocol.NewError("invalid_argument", "The JSON input does not match its command schema.", 2, map[string]any{"reason": issues[0].Reason, "sources": []any{map[string]any{"kind": "json", "path": append([]string{}, issues[0].Path...)}}, "facts": map[string]any{"rule": issues[0].Rule}})
	}
	return v.(map[string]any), nil
}
func (a *App) configInput(r Request) (configedit.Input, *protocol.Error) {
	dirs, err := a.currentPaths()
	if err != nil {
		return configedit.Input{}, directoryError(err)
	}
	return configedit.Input{Revision: r.Options["revision"], DryRun: r.Options["dry-run"] == "true", Runtime: dirs.Runtime}, nil
}
func (a *App) configAction(action string) func(context.Context, IO, Request) (any, *protocol.Error) {
	return func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		in, e := a.configInput(r)
		if e != nil {
			return nil, e
		}
		if action == "fmt" && r.Options["check"] == "true" {
			if in.DryRun {
				return nil, argumentError("--check and --dry-run cannot both be true.", "check")
			}
			in.DryRun = true
		}
		if action == "migrate" {
			if r.Options["apply"] != "" {
				if in.DryRun || in.Revision == "" {
					return nil, argumentError("Migration apply requires --revision and cannot use --dry-run=true.", "apply")
				}
				in.ApplyDigest = r.Options["apply"]
			} else {
				in.DryRun = true
			}
			in.Migrate = true
		}
		if action == "edit" {
			body, e := readConfigBody(ctx, streams, r, "Edit")
			if e != nil {
				return nil, e
			}
			b, _ := json.Marshal(body["operations"])
			_ = json.Unmarshal(b, &in.Operations) // Decode values separately to retain int64.
			for i, v := range body["operations"].([]any) {
				if value, ok := v.(map[string]any)["value"].(map[string]any); ok {
					in.Operations[i].Value = value
				}
			}
		}
		path, e := project.Find(r.Options["dir"])
		if e != nil {
			return nil, e
		}
		switch action {
		case "inspect":
			return configedit.Inspect(path)
		case "check":
			return configedit.Check(path)
		case "fmt":
			in.Format = true
		}
		result, e := configedit.Evaluate(ctx, path, in)
		if e != nil {
			return nil, e
		}
		if action == "fmt" && r.Options["check"] == "true" && result.(configedit.Result).WouldChange {
			return nil, protocol.NewError("config_format_required", "Formatting is required. Run devtools config fmt after reviewing this preview.", 3, map[string]any{"reason": "formatting_required", "report": result})
		}
		return result, nil
	}
}
func (a *App) configResource(kind, action string) func(context.Context, IO, Request) (any, *protocol.Error) {
	return func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		in, e := a.configInput(r)
		if e != nil {
			return nil, e
		}
		op := configedit.Operation{Op: kind + "." + action, Name: r.Args[0]}
		if action == "rename" {
			op.NewName = r.Args[1]
		}
		if action == "add" || action == "update" {
			def := strings.ToUpper(kind[:1]) + kind[1:]
			if action == "update" {
				def += "Patch"
			}
			if len(r.Child) > 0 {
				if kind != "command" || action != "add" || r.Options["file"] != "" || r.Options["stdin"] == "true" {
					return nil, argumentError("Select JSON input or -- PROGRAM ARG..., once.", "file")
				}
				args := []any{}
				for _, arg := range r.Child {
					args = append(args, arg)
				}
				op.Value = map[string]any{"exec": args}
			} else {
				op.Value, e = readConfigBody(ctx, streams, r, def)
				if e != nil {
					return nil, e
				}
			}
		}
		path, e := project.Find(r.Options["dir"])
		if e != nil {
			return nil, e
		}
		in.Operations = []configedit.Operation{op}
		return configedit.Evaluate(ctx, path, in)
	}
}

func (a *App) explainCommand(_ context.Context, _ IO, r Request) (any, *protocol.Error) {
	path, e := project.Find(r.Options["dir"])
	if e != nil {
		return nil, e
	}
	s, e := projectfile.Read(path)
	if e != nil {
		return nil, e
	}
	document, e := project.ParseDocument(s.Bytes, path, filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	p := document.Context
	if s := r.Options["profile"]; s != "" {
		p.Profile = s
	}
	var env *string
	if v, ok := r.Options["env"]; ok {
		env = &v
	}
	resolved, e := execution.ResolveConfigured(p, r.Args[0], env, nil)
	if e != nil {
		return nil, e
	}
	if s := r.Options["bundle"]; s != "" {
		resolved.Selection = s
	}
	details := describeProjectCommand(resolved.Name, p.Commands[resolved.Name], p.Requirements)
	details.Selection = resolved.Selection
	details.Env = resolved.Env
	details.Inject = resolved.Inject
	overrides := map[string]configcontract.Source{}
	for _, field := range []string{"env", "bundle"} {
		if _, ok := r.Options[field]; ok {
			source := configcontract.Source{Kind: "argv", Path: []string{field}}
			if offset, ok := r.OptionOffsets[field]; ok {
				index := offset
				source.Offset = &index
			}
			overrides[field] = source
			if field == "env" {
				overrides["inject"] = source
			}
		}
	}
	unresolved := []any{}
	diagnosis := []string{"devtools", "doctor", "--dir", p.Root, "--profile", p.Profile, "--bundle", resolved.Selection, "--mode", "foreground"}
	if resolved.Inject {
		diagnosis = append(diagnosis, "--env", resolved.Env)
	}
	diagnosis = append(diagnosis, resolved.Name)
	for _, field := range []string{"bind", "serve", "values"} {
		needed := field == "bind" && len(resolved.Bind) > 0 || field == "serve" && len(resolved.Serve) > 0 || field == "values" && resolved.NeedsValues()
		if needed {
			unresolved = append(unresolved, map[string]any{"code": "dynamic_input_unresolved", "reason": "requires_preflight", "message": "Runtime value/network inputs are resolved by doctor and execution preflight.", "target": map[string]any{"kind": "command", "name": resolved.Name, "profile": p.Profile}, "sources": []configcontract.Source{}, "facts": map[string]any{"field": field}, "remedies": []protocol.Remedy{doctor.Remedy("Inspect this command without starting the application.", diagnosis)}})
		}
	}
	item := map[string]any{"name": details.Name, "exec": details.Exec, "inject": details.Inject, "env": details.Env, "selection": details.Selection, "output": map[string]any{"mode": details.Output.Mode, "stdout": details.Output.Stdout, "stderr": details.Output.Stderr}, "values_update": details.ValuesUpdate, "serve": details.Serve, "bind": details.Bind, "ready": details.Ready, "requirements": details.Requirements}
	return map[string]any{"config_path": p.ConfigPath, "revision": s.Revision, "profile": p.Profile, "directory": filepath.Dir(p.ConfigPath), "item": item, "provenance": p.Document.ProvenanceFor(resolved.Name, overrides), "unresolved": unresolved}, nil
}
