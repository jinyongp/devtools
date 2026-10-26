package presentation

import (
	"fmt"

	"github.com/jinyongp/devtools/internal/cliui"
)

// These are canonical command names, not heuristics over response keys or
// substrings. Newly registered commands retain the complete generic fallback.
var mutationCommands = []string{
	"init", "vault init", "vault recover", "vault rewrap", "vault unlock", "vault lock",
	"variable set", "variable unset", "secret set", "secret unset", "env create", "env remove", "secret generate",
	"config fmt", "config edit", "config migrate", "config tool add", "config tool update", "config tool remove",
	"config preset add", "config preset update", "config preset remove", "command add", "command update", "command remove", "command rename",
	"import", "bundle set", "bundle rename", "bundle remove", "values execution approve", "values execution cancel",
	"values execution deliver", "values execution report", "values execution prepare", "values runner address", "values runner register", "values runner unregister",
	"dashboard", "dashboard stop", "port allocate", "port release", "port prune", "instance name", "instance move", "instance remove",
	"proxy start", "proxy stop", "proxy setup", "proxy teardown", "proxy configure", "proxy restart", "proxy hosts apply", "proxy hosts remove",
	"expose start", "expose stop", "proxy add", "proxy rm", "proxy clear", "proxy up", "proxy down", "proxy ca init", "proxy ca export", "proxy trust", "proxy untrust",
	"backup keygen", "backup configure", "backup create", "backup restore", "profile diff", "profile transfer prepare", "profile export", "profile import",
	"process start", "process stop", "process restart", "project up", "project restart", "project down", "cleanup apply", "cleanup archives", "cleanup restore", "cleanup purge", "update",
}

var taskCommands = []string{
	"task sync", "task workstream edit", "task workstream create", "task workstream update", "task add", "task update",
	"task workstream spec set", "task workstream plan set", "task depends set", "task workstream depends set", "task attach", "task detach",
	"task hold", "task unhold", "task cancel", "task reopen", "task workstream cancel", "task workstream reopen", "task workstream activate", "task workstream close",
	"task claim", "task resume", "task takeover", "task unclaim", "task checkpoint", "task release", "task done",
	"task validation add", "task validation update", "task validation basis", "task validation record", "task validation accept", "task validation waive", "task validation unwaive",
	"task list", "task show", "task next", "task current", "task context", "task history", "task impact", "task tree", "task export", "task checkpoint list",
	"task workstream list", "task workstream show", "task workstream context", "task workstream history", "task workstream impact", "task workstream tree", "task workstream export",
	"task workstream check", "task workstream spec show", "task workstream plan show", "task validation list", "task validation show",
}

func operationDefinition(command string) Definition {
	if command == "" {
		return Definition{}
	}
	base := Definition{Title: title(command), Required: []Requirement{{"", "object"}}}
	switch command {
	case "doctor":
		return Definition{Family: "diagnostic", Title: "Doctor", Required: []Requirement{{"state", "string"}, {"ready", "boolean"}, {"checks", "array"}, {"commands", "array"}}, Optional: []string{"summary", "scope", "selection", "mode", "profile", "directory", "config_path"}}
	case "diagnostics":
		base.Family = "diagnostic"
		base.Required = []Requirement{{"ready", "boolean"}, {"issues", "array"}}
		return base
	case "process check", "process wait":
		base.Family = "diagnostic"
		base.Required = []Requirement{{"id", "string"}, {"ready_configured", "boolean"}, {"readiness", "object"}, {"readiness.ready", "boolean"}}
		return base
	case "config check", "port check":
		base.Family = "diagnostic"
		return base
	case "cleanup preview", "proxy hosts preview":
		base.Family = "preview"
		return base
	}
	for _, name := range mutationCommands {
		if name == command {
			base.Family = "mutation"
			base.Optional = []string{"changed", "replayed", "dry_run", "item", "items", "revision", "request_id", "effects", "retry", "next_actions", "warnings"}
			base.KnownOmitted = []Omission{{"warnings", "empty-array"}}
			return base
		}
	}
	for _, name := range taskCommands {
		if name == command {
			base.Family = "task"
			base.Required = []Requirement{{"profile", "string"}, {"revision", "number"}}
			base.Optional = []string{"item", "items", "tasks", "validations", "documents", "nodes", "edges", "next_actions", "valid", "execution_ready", "closable", "changed", "replayed", "dry_run"}
			return base
		}
	}
	return Definition{}
}

func boolField(data map[string]any, key string) bool { value, _ := data[key].(bool); return value }

func operationView(definition Definition, data map[string]any) (cliui.Document, bool) {
	for _, key := range []string{"changed", "replayed", "dry_run"} {
		if value, exists := data[key]; exists {
			if _, ok := value.(bool); !ok {
				return cliui.Document{}, false
			}
		}
	}
	var spans []cliui.Span
	label := func(text string, role cliui.Role) { spans = append(spans, cliui.Span{Text: text + "  ", Role: role}) }
	preview := definition.Family == "preview" || boolField(data, "dry_run")
	replayed := boolField(data, "replayed")
	if preview {
		label("PREVIEW", cliui.Info)
	}
	if replayed {
		label("REPLAYED", cliui.Primary)
	}
	if !preview && !replayed && (definition.Family == "mutation" || definition.Family == "task") {
		if changed, exists := data["changed"].(bool); exists {
			if changed {
				label("CHANGED", cliui.Success)
			} else {
				label("NO CHANGE", cliui.Primary)
			}
		}
	}
	if definition.Family == "diagnostic" {
		state, _ := data["state"].(string)
		if state != "" {
			text, role := stateLabel(state)
			label(text, role)
		} else if ready, exists := data["ready"].(bool); exists {
			if ready {
				label("READY", cliui.Success)
			} else {
				label("NOT READY", cliui.Failure)
			}
		} else if readiness, exists := data["readiness"].(map[string]any); exists {
			if ready, valid := readiness["ready"].(bool); valid {
				if ready {
					label("READY", cliui.Success)
				} else {
					label("NOT READY", cliui.Failure)
				}
			}
		} else if valid, exists := data["valid"].(bool); exists {
			if valid {
				label("VALID", cliui.Success)
			} else {
				label("INVALID", cliui.Failure)
			}
		}
	}
	spans = append(spans, cliui.Span{Text: definition.Title})
	blocks := []cliui.Block{cliui.Heading(cliui.Text{Spans: spans, Flow: cliui.Verbatim}, 1)}
	consumed := map[string]bool{}
	blocks = append(blocks, scope(data, consumed)...)
	if item, exists := data["item"].(map[string]any); exists {
		consumed["item"] = true
		if definition.Family == "task" {
			blocks = append(blocks, taskItem(item)...)
		} else {
			blocks = append(blocks, detail(item, "item", definition)...)
		}
	}
	for _, key := range []string{"changed", "dry_run", "replayed", "ready", "valid", "execution_ready", "closable", "state", "effects", "retry"} {
		if value, exists := data[key]; exists {
			consumed[key] = true
			if scalar(value) {
				blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal(title(key)), Value: display(value)}))
			} else {
				blocks = append(blocks, named(key, value)...)
			}
		}
	}
	for _, key := range []string{"items", "checks", "commands", "issues", "warnings", "unresolved", "tasks", "nodes", "edges", "blockers", "validations", "documents", "readiness"} {
		value, exists := data[key]
		if !exists || omit(key, value, definition) {
			continue
		}
		consumed[key] = true
		if definition.Family == "task" && key == "documents" {
			blocks = append(blocks, documentBodies(value)...)
		} else if definition.Family == "task" && (key == "items" || key == "tasks" || key == "validations" || key == "nodes") {
			if rows, ok := value.([]any); ok {
				var records []cliui.Block
				for i, row := range rows {
					children := tree(row)
					if object, valid := row.(map[string]any); valid {
						children = taskItem(object)
					}
					records = append(records, cliui.Section(cliui.Literal(fmt.Sprintf("[%d]", i)), children...))
				}
				if len(records) == 0 {
					records = tree(value)
				}
				blocks = append(blocks, cliui.Section(cliui.Literal(title(key)), records...))
			} else {
				blocks = append(blocks, named(key, value)...)
			}
		} else {
			blocks = append(blocks, named(key, value)...)
		}
	}
	blocks = append(blocks, identity(data, consumed)...)
	// identity's fields may already have been shown as operation state.
	blocks = append(blocks, remaining(data, consumed, "", definition)...)
	return cliui.NewDocument(blocks...), true
}

func stateLabel(state string) (string, cliui.Role) {
	switch state {
	case "pass":
		return "OK", cliui.Success
	case "ready":
		return "READY", cliui.Success
	case "fail":
		return "FAIL", cliui.Failure
	case "not_ready":
		return "NOT READY", cliui.Failure
	case "unknown":
		return "UNKNOWN", cliui.Warning
	case "skipped":
		return "SKIPPED", cliui.Primary
	case "not_applicable":
		return "NOT APPLICABLE", cliui.Primary
	}
	return state, cliui.Primary
}

func diagnosticSection(key string, value any) ([]cliui.Block, bool) {
	switch key {
	case "warnings", "checks", "causes", "issues", "unresolved", "pending":
	default:
		return nil, false
	}
	array, ok := value.([]any)
	if !ok {
		return nil, false
	}
	var children []cliui.Block
	for i, value := range array {
		row, valid := value.(map[string]any)
		if !valid {
			children = append(children, cliui.Section(cliui.Literal(fmt.Sprintf("[%d]", i)), tree(value)...))
			continue
		}
		label, role := "", cliui.Primary
		if key == "warnings" {
			label, role = "WARN", cliui.Warning
		} else if state, valid := row["status"].(string); valid && key == "checks" {
			label, role = stateLabel(state)
		}
		spans := []cliui.Span{}
		if label != "" {
			spans = append(spans, cliui.Span{Text: label + "  ", Role: role})
		}
		for _, field := range []string{"id", "code"} {
			if text, valid := row[field].(string); valid {
				spans = append(spans, cliui.Span{Text: text + "  "})
			}
		}
		if len(spans) == 0 {
			spans = append(spans, cliui.Span{Text: fmt.Sprintf("[%d]", i)})
		}
		consumed := map[string]bool{}
		var content []cliui.Block
		if message, valid := row["message"].(string); valid {
			consumed["message"] = true
			content = append(content, cliui.Paragraph(cliui.Words(message)))
		}
		// Original status/id/code remain in the record as well as its visual label.
		content = append(content, remaining(row, consumed, "", Definition{})...)
		children = append(children, cliui.Section(cliui.Text{Spans: spans}, content...))
	}
	if len(children) == 0 {
		children = tree(value)
	}
	return []cliui.Block{cliui.Section(cliui.Literal(title(key)), children...)}, true
}

func taskItem(item map[string]any) []cliui.Block {
	consumed := map[string]bool{}
	var fields []cliui.Field
	for _, key := range []string{"id", "title", "kind", "state", "completion_status", "execution_status", "running", "ready"} {
		if value, exists := item[key]; exists && scalar(value) {
			consumed[key] = true
			fields = append(fields, cliui.Field{Label: cliui.Literal(title(key)), Value: display(value)})
		}
	}
	var result []cliui.Block
	if len(fields) > 0 {
		result = append(result, cliui.Fields(fields...))
	}
	if description, valid := item["description"].(string); valid {
		consumed["description"] = true
		result = append(result, cliui.Section(cliui.Literal("Description"), cliui.Paragraph(cliui.Literal(description))))
	}
	return append(result, remaining(item, consumed, "", Definition{})...)
}

func documentBodies(value any) []cliui.Block {
	documents, valid := value.(map[string]any)
	if !valid {
		return named("documents", value)
	}
	var sections []cliui.Block
	for _, key := range keys(documents) {
		object, valid := documents[key].(map[string]any)
		if !valid {
			sections = append(sections, named(key, documents[key])...)
			continue
		}
		consumed := map[string]bool{}
		var blocks []cliui.Block
		if body, valid := object["body"].(string); valid {
			consumed["body"] = true
			if body == "" {
				body = `""`
			}
			blocks = append(blocks, cliui.Paragraph(cliui.Literal(body)))
		}
		blocks = append(blocks, remaining(object, consumed, "", Definition{})...)
		sections = append(sections, cliui.Section(cliui.Literal(title(key)), blocks...))
	}
	return []cliui.Block{cliui.Section(cliui.Literal("Documents"), sections...)}
}
