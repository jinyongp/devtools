package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) taskStore(r Request) (tasks.Store, *protocol.Error) {
	p, e := project.Resolve(".", r.Options["profile"])
	if e != nil {
		return tasks.Store{}, e
	}
	dir, e := a.dataDirectory()
	if e != nil {
		return tasks.Store{}, e
	}
	dirs, err := paths.Current()
	if err != nil {
		return tasks.Store{}, argumentError("Cannot resolve cache directory.", "")
	}
	return tasks.Store{Directory: filepath.Join(filepath.Dir(dir), "tasks"), Profile: p.Profile, Cache: filepath.Join(dirs.Cache, "task-queries")}, nil
}
func taskOptions() []Option {
	return []Option{{Name: "profile", Description: "Explicit project profile.", Pattern: project.ProfilePattern, MaxLength: 128}}
}
func taskField(k string) string {
	switch k {
	case "workstream_id":
		return "workstream"
	case "task_id":
		return "task"
	case "basis_id":
		return "basis"
	case "record_id":
		return "record"
	}
	return strings.ReplaceAll(k, "_", "-")
}
func taskArray(k string) bool {
	return strings.HasSuffix(k, "_ids") || strings.HasSuffix(k, "_keys") || k == "depends_on" || k == "acceptance" || k == "decisions" || k == "remaining" || k == "blockers"
}
func structured(k string) bool {
	return k == "operations" || k == "requirements" || k == "code" || k == "evidence" || k == "commits"
}
func (a *App) registerTasks() {
	for _, d := range tasks.Definitions {
		def := d
		opts := taskOptions()
		opts = append(opts, Option{Name: "request-id", Required: def.Action != "workstream.edited", Description: "UUID for idempotent retries; required for writes."}, Option{Name: "file", Description: "JSON request file."}, Option{Name: "stdin", Boolean: true, Description: "Read JSON from a pipe."})
		if def.Action == "workstream.edited" {
			opts = append(opts, Option{Name: "dry-run", Boolean: true, Description: "Preview the edit without saving or consuming a request ID."})
		}
		if def.Action == "validation.basis" {
			opts = append(opts, Option{Name: "if-revision", Description: "Observed profile revision; required for workstream validation."})
		}
		if def.Revision {
			opts = append(opts, Option{Name: "if-revision", Required: true, Description: "Latest profile revision."})
		}
		opts = append(opts, Option{Name: "context", Description: "Current execution context; defaults to DEVTOOLS_TASK_CONTEXT."})
		if def.Action == "run.revoked" {
			opts = append(opts, Option{Name: "expected-run", Required: true, Description: "Observed current run UUID to revoke."})
		}
		if def.Action == "run.claimed" || def.Action == "run.taken_over" {
			opts = append(opts, Option{Name: "dir", Default: ".", Description: "Execution working directory."})
			if def.Action == "run.claimed" {
				opts = append(opts, Option{Name: "workstream", Description: "Select ready tasks in this workstream."})
			} else {
				opts = append(opts, Option{Name: "expected-run", Required: true, Description: "Observed current run UUID."})
			}
		}
		for _, f := range def.Fields {
			if structured(f) || f == "acceptance" && def.Action == "spec.set" {
				continue
			}
			opts = append(opts, Option{Name: taskField(f), Repeatable: taskArray(f), Boolean: f == "required", Description: "Request field: " + f})
		}
		args := []Argument{}
		if def.Target || def.OptionalTarget {
			args = append(args, Argument{Name: "id", Required: def.Target})
		}
		a.commands = append(a.commands, Command{Name: "task " + def.Command, Description: "Apply " + def.Action + " atomically.", Options: opts, Arguments: args, BodySchema: taskBody(def), Output: taskOutput(true), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			store, e := a.taskStore(r)
			if e != nil {
				return nil, e
			}
			b := tasks.Object{}
			jsonInput := r.Options["file"] != "" || r.Options["stdin"] == "true"
			if jsonInput {
				for _, f := range def.Fields {
					if _, exists := r.Options[taskField(f)]; exists {
						return nil, argumentError("Choose JSON input or field options.", f)
					}
				}
				data, e := taskInput(streams, r)
				if e != nil {
					return nil, e
				}
				b, e = tasks.Decode(data)
				if e != nil {
					return nil, e
				}
			} else {
				for _, f := range def.Fields {
					n := taskField(f)
					if v, ok := r.Options[n]; ok {
						if taskArray(f) {
							b[f] = r.ListOptions[n]
						} else if f == "required" {
							b[f] = v == "true"
						} else {
							b[f] = v
						}
					}
				}
			}
			target := ""
			if len(r.Args) > 0 {
				target = r.Args[0]
			}
			if def.Action == "run.claimed" && target != "" && r.Options["workstream"] != "" {
				return nil, argumentError("Choose a task ID or workstream filter.", "workstream")
			}
			if r.Options["context"] == "" {
				r.Options["context"] = os.Getenv("DEVTOOLS_TASK_CONTEXT")
			}
			if dir := r.Options["dir"]; dir != "" {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return nil, argumentError("Cannot resolve directory.", "dir")
				}
				r.Options["dir"] = abs
			}
			return store.Execute(ctx, tasks.Request{Action: def.Action, Target: target, Body: b, Options: r.Options})
		}})
		if def.Action == "workstream.edited" {
			a.commands[len(a.commands)-1].InputOneOf = []map[string]any{
				{"required": []string{"dry-run"}, "properties": map[string]any{"dry-run": map[string]any{"const": true}}},
				{"required": []string{"request-id"}, "not": map[string]any{"required": []string{"dry-run"}, "properties": map[string]any{"dry-run": map[string]any{"const": true}}}},
			}
		}
	}
	for _, name := range []string{"list", "show", "next", "current", "context", "history", "impact", "tree", "export", "checkpoint list", "workstream list", "workstream show", "workstream context", "workstream history", "workstream impact", "workstream tree", "workstream export", "workstream check", "workstream spec show", "workstream plan show", "validation list", "validation show"} {
		name := name
		opts := taskOptions()
		for _, n := range []string{"state", "workstream", "limit", "cursor", "direction", "depth", "dir"} {
			opts = append(opts, Option{Name: n, Description: "Query option: " + n})
		}
		if name == "list" || name == "validation list" {
			opts = append(opts, Option{Name: "scope", Description: "included (default), removed, or all."})
		}
		if name == "list" || name == "workstream list" {
			opts = append(opts, Option{Name: "completion", Description: "none, current, stale, or all."})
		}
		if name == "workstream plan show" {
			opts = append(opts, Option{Name: "at-revision", Description: "Read a complete historical profile revision."})
		}
		if strings.HasSuffix(name, "impact") {
			opts = append(opts, Option{Name: "file", Description: "Proposed definition changes as JSON."}, Option{Name: "stdin", Boolean: true, Description: "Read proposed changes from a pipe."})
		}
		required := strings.HasSuffix(name, "show") || strings.HasSuffix(name, "context") || strings.HasSuffix(name, "impact") || strings.HasSuffix(name, "export") || strings.HasSuffix(name, "check") || name == "checkpoint list"
		args := []Argument{}
		if required || strings.HasSuffix(name, "history") || strings.HasSuffix(name, "tree") {
			args = append(args, Argument{Name: "id", Required: required})
		}
		a.commands = append(a.commands, Command{Name: "task " + name, Description: "Read task domain: " + name + ".", Options: opts, Arguments: args, Output: taskOutput(false), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			store, e := a.taskStore(r)
			if e != nil {
				return nil, e
			}
			target := ""
			if len(r.Args) > 0 {
				target = r.Args[0]
			}
			var body tasks.Object
			if r.Options["file"] != "" || r.Options["stdin"] == "true" {
				raw, e := taskInput(streams, r)
				if e != nil {
					return nil, e
				}
				body, e = tasks.Decode(raw)
				if e != nil {
					return nil, e
				}
			}
			return store.Query(tasks.Query{Command: name, Target: target, Options: r.Options, Body: body})
		}})
	}
}
func taskInput(streams IO, r Request) (string, *protocol.Error) {
	if r.Options["file"] != "" && r.Options["stdin"] == "true" {
		return "", argumentError("Choose file or stdin.", "")
	}
	reader := streams.In
	var f *os.File
	if path := r.Options["file"]; path != "" {
		var e error
		f, e = os.Open(path)
		if e != nil {
			return "", argumentError("Cannot open JSON file.", "file")
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			return "", argumentError("Use a regular JSON file.", "file")
		}
		reader = f
	} else if input, ok := reader.(*os.File); ok {
		info, e := input.Stat()
		if e != nil || info.Mode()&os.ModeCharDevice != 0 {
			return "", argumentError("Provide JSON through a pipe or redirected file.", "stdin")
		}
	}
	b, e := io.ReadAll(io.LimitReader(reader, (2<<20)+1))
	if e != nil {
		return "", argumentError("Cannot read JSON input.", "")
	}
	return string(b), nil
}
