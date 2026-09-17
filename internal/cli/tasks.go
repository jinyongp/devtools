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
func taskTargetArgument(kind string) string {
	return kind + "-id"
}
func taskFieldDescription(field string) string {
	descriptions := map[string]string{
		"title":                 "Human-readable title.",
		"description":           "Human-readable description; an empty value clears it.",
		"body":                  "Markdown document body.",
		"workstream_id":         "Owning workstream UUID.",
		"task_id":               "Owning task UUID.",
		"acceptance":            "Acceptance condition; repeat for multiple conditions.",
		"acceptance_keys":       "Workstream acceptance key; repeat for multiple keys.",
		"task_ids":              "Included task UUID; repeat for multiple tasks.",
		"validation_ids":        "Included validation UUID; repeat for multiple validations.",
		"depends_on":            "Prerequisite UUID; repeat for multiple prerequisites.",
		"reason":                "Reason recorded in history.",
		"summary":               "Progress or outcome summary.",
		"decisions":             "Decision and rationale; repeat for multiple decisions.",
		"validation_record_ids": "Validation record UUID; repeat for multiple records.",
		"remaining":             "Remaining work item; repeat for multiple items.",
		"next_action":           "Recommended next action.",
		"blockers":              "Current blocker; repeat for multiple blockers.",
		"method":                "How this validation is performed.",
		"required":              "Whether this validation is required.",
		"result":                "Validation result: pass, fail, blocked, or skipped.",
		"basis_id":              "Validation basis UUID.",
		"record_id":             "Validation record UUID.",
	}
	return descriptions[field]
}
func taskMutationDescription(action string) string {
	descriptions := map[string]string{
		"run.synced":          "Acknowledge task definition changes for the current run.",
		"workstream.edited":   "Apply a batch of workstream definition changes atomically.",
		"workstream.create":   "Create a workstream.",
		"workstream.update":   "Update workstream title or description.",
		"task.add":            "Create a task.",
		"task.update":         "Update task title, description, or acceptance conditions.",
		"spec.set":            "Replace a workstream specification.",
		"plan.set":            "Replace a workstream implementation plan.",
		"task.depends":        "Replace a task's prerequisite list.",
		"workstream.depends":  "Replace a workstream's prerequisite list.",
		"task.attach":         "Attach a task to a workstream.",
		"task.detach":         "Detach a task from its workstream.",
		"task.hold":           "Put a task on hold.",
		"task.unhold":         "Remove a task hold.",
		"task.cancel":         "Cancel a task.",
		"task.reopen":         "Reopen a canceled or completed task.",
		"workstream.cancel":   "Cancel a workstream.",
		"workstream.reopen":   "Reopen a canceled or closed workstream.",
		"workstream.activate": "Activate a structurally valid workstream for execution.",
		"workstream.close":    "Close a workstream whose work and validations are current.",
		"run.claimed":         "Claim a ready task and start a run.",
		"run.resumed":         "Resume the run identified by the current execution context.",
		"run.taken_over":      "Replace a task's active run with a new run.",
		"run.revoked":         "Administratively revoke a task's active run.",
		"run.checkpointed":    "Save progress for the run identified by the run UUID.",
		"run.released":        "Save optional progress and release the run identified by the run UUID.",
		"task.completed":      "Complete a task using its current run and validation evidence.",
		"validation.add":      "Create a validation definition.",
		"validation.update":   "Update a validation definition.",
		"validation.basis":    "Record the code and definition basis for a validation run.",
		"validation.record":   "Record a validation result and its evidence.",
		"validation.accept":   "Accept a reusable validation result for the current run.",
		"validation.waive":    "Waive a validation with a recorded reason.",
		"validation.unwaive":  "Remove a validation waiver with a recorded reason.",
	}
	return descriptions[action]
}
func taskQueryDescription(command string) string {
	descriptions := map[string]string{
		"list":                 "List tasks.",
		"show":                 "Show one task.",
		"next":                 "Show the oldest task currently ready to claim.",
		"current":              "Show runs associated with an execution directory.",
		"context":              "Show task goals, definitions, execution state, and next actions.",
		"history":              "List task history.",
		"impact":               "Show downstream impact and execution blockers for a task.",
		"tree":                 "Show task dependency relationships.",
		"export":               "Export one task and its public history as JSON.",
		"checkpoint list":      "List saved progress for one run.",
		"workstream list":      "List workstreams.",
		"workstream show":      "Show one workstream.",
		"workstream context":   "Show workstream goals, documents, tasks, and validations.",
		"workstream history":   "List workstream history.",
		"workstream impact":    "Show downstream impact and execution blockers for a workstream.",
		"workstream tree":      "Show workstream dependency relationships.",
		"workstream export":    "Export one workstream and its public history as JSON.",
		"workstream check":     "Check workstream structure and coverage.",
		"workstream spec show": "Show a workstream specification.",
		"workstream plan show": "Show a workstream implementation plan.",
		"validation list":      "List validation definitions.",
		"validation show":      "Show one validation definition and its current evidence state.",
	}
	return descriptions[command]
}
func taskQueryOptionDescription(option string) string {
	descriptions := map[string]string{
		"state":      "Filter by an allowed lifecycle state.",
		"workstream": "Filter results by workstream UUID.",
		"limit":      "Maximum number of results, from 1 to 200; defaults to 50.",
		"cursor":     "Continue a previous paginated query.",
		"direction":  "Dependency direction: upstream or downstream; defaults to upstream.",
		"depth":      "Maximum dependency depth, from 1 to 20; defaults to 3.",
		"dir":        "Execution directory used by current-run queries; defaults to the current directory.",
	}
	return descriptions[option]
}
func taskQueryOptionPattern(command, option string) string {
	switch option {
	case "state":
		if command == "workstream list" {
			return `^(draft|active|done|canceled|all)$`
		}
		return `^(open|done|canceled|all)$`
	case "workstream":
		return uuidPattern
	case "limit":
		return `^([1-9]|[1-9][0-9]|1[0-9]{2}|200)$`
	case "direction":
		return `^(upstream|downstream)$`
	case "depth":
		return `^([1-9]|1[0-9]|20)$`
	}
	return ""
}
func taskWorkstreamFiltersResults(command string) bool {
	return command == "list" || command == "next" || command == "tree" || command == "validation list"
}
func taskQueryOptions(command string) []string {
	switch command {
	case "list":
		return []string{"state", "workstream", "limit", "cursor"}
	case "validation list":
		return []string{"workstream", "limit", "cursor"}
	case "workstream list":
		return []string{"state", "limit", "cursor"}
	case "next":
		return []string{"workstream"}
	case "show", "context", "impact", "export":
		return []string{"workstream"}
	case "current":
		return []string{"limit", "cursor", "dir"}
	case "history":
		return []string{"workstream", "limit", "cursor"}
	case "workstream history", "checkpoint list":
		return []string{"limit", "cursor"}
	case "tree":
		return []string{"workstream", "limit", "cursor", "direction", "depth"}
	case "workstream tree":
		return []string{"limit", "cursor", "direction", "depth"}
	default:
		return nil
	}
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
		opts = append(opts, Option{Name: "request-id", Required: def.Action != "workstream.edited", Pattern: uuidPattern, Description: "UUID for idempotent retries; required for writes."}, Option{Name: "file", Description: "JSON request file."}, Option{Name: "stdin", Boolean: true, Description: "Read JSON from a pipe."})
		if def.Action == "workstream.edited" {
			opts = append(opts, Option{Name: "dry-run", Boolean: true, Description: "Preview the edit without saving or consuming a request ID."})
		}
		if def.Action == "validation.basis" {
			opts = append(opts, Option{Name: "if-revision", Pattern: positiveIntegerPattern, Description: "Observed positive profile revision; required for workstream validation."})
		}
		if def.Revision {
			opts = append(opts, Option{Name: "if-revision", Required: true, Pattern: positiveIntegerPattern, Description: "Latest positive profile revision."})
		}
		contextCapable := def.Context || def.ContextGuard || def.ContextOwner
		if contextCapable {
			opts = append(opts, Option{Name: "context", Description: "Current execution context; defaults to DEVTOOLS_TASK_CONTEXT."})
		}
		if def.Action == "run.revoked" {
			opts = append(opts, Option{Name: "expected-run", Required: true, Pattern: uuidPattern, Description: "Observed current run UUID to revoke."})
		}
		if def.Action == "run.claimed" || def.Action == "run.taken_over" {
			opts = append(opts, Option{Name: "dir", Default: ".", Description: "Execution working directory."})
			if def.Action == "run.claimed" {
				opts = append(opts, Option{Name: "workstream", Pattern: uuidPattern, Description: "Select ready tasks in this workstream."})
			} else {
				opts = append(opts, Option{Name: "expected-run", Required: true, Pattern: uuidPattern, Description: "Observed current run UUID."})
			}
		}
		for _, f := range def.Fields {
			if structured(f) || f == "acceptance" && def.Action == "spec.set" {
				continue
			}
			pattern := ""
			if strings.HasSuffix(f, "_id") || strings.HasSuffix(f, "_ids") || f == "depends_on" {
				pattern = uuidPattern
			} else if f == "result" {
				pattern = `^(pass|fail|blocked|skipped)$`
			}
			opts = append(opts, Option{Name: taskField(f), Repeatable: taskArray(f), Boolean: f == "required", Pattern: pattern, Description: taskFieldDescription(f)})
		}
		args := []Argument{}
		if def.Target || def.OptionalTarget {
			args = append(args, Argument{Name: taskTargetArgument(def.Kind), Required: def.Target, Pattern: uuidPattern})
		}
		a.commands = append(a.commands, Command{Name: "task " + def.Command, Description: taskMutationDescription(def.Action), Options: opts, Arguments: args, BodySchema: taskBody(def), Output: taskOutput(true), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
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
			if contextCapable && r.Options["context"] == "" {
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
		for _, n := range taskQueryOptions(name) {
			pattern := taskQueryOptionPattern(name, n)
			description := taskQueryOptionDescription(n)
			if n == "state" {
				if name == "workstream list" {
					description = "Filter by lifecycle state: draft, active, done, canceled, or all."
				} else {
					description = "Filter by lifecycle state: open, done, canceled, or all."
				}
			} else if n == "workstream" {
				if name == "tree" {
					description = "Filter root tasks when no task UUID is supplied; narrow task ID completion otherwise."
				} else if !taskWorkstreamFiltersResults(name) {
					description = "Narrow task ID completion to this workstream; does not filter the command result."
				}
			}
			opts = append(opts, Option{Name: n, Pattern: pattern, Description: description})
		}
		if name == "list" || name == "validation list" {
			opts = append(opts, Option{Name: "scope", Pattern: `^(included|removed|all)$`, Description: "Scope: included (default), removed, or all."})
		}
		if name == "list" || name == "workstream list" {
			opts = append(opts, Option{Name: "completion", Pattern: `^(none|current|stale|all)$`, Description: "Completion status: none, current, stale, or all."})
		}
		if name == "workstream plan show" {
			opts = append(opts, Option{Name: "at-revision", Pattern: positiveIntegerPattern, Description: "Read an existing positive, complete historical profile revision."})
		}
		if strings.HasSuffix(name, "impact") {
			opts = append(opts, Option{Name: "file", Description: "Proposed definition changes as JSON."}, Option{Name: "stdin", Boolean: true, Description: "Read proposed changes from a pipe."})
		}
		required := strings.HasSuffix(name, "show") || strings.HasSuffix(name, "context") || strings.HasSuffix(name, "impact") || strings.HasSuffix(name, "export") || strings.HasSuffix(name, "check") || name == "checkpoint list"
		args := []Argument{}
		if required || strings.HasSuffix(name, "history") || strings.HasSuffix(name, "tree") {
			kind := "task"
			switch {
			case name == "checkpoint list":
				kind = "run"
			case strings.HasPrefix(name, "workstream "):
				kind = "workstream"
			case strings.HasPrefix(name, "validation "):
				kind = "validation"
			}
			args = append(args, Argument{Name: taskTargetArgument(kind), Required: required, Pattern: uuidPattern})
		}
		a.commands = append(a.commands, Command{Name: "task " + name, Description: taskQueryDescription(name), Options: opts, Arguments: args, Output: taskQueryOutput(name), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
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
