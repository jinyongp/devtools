package presentation

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

// Text diagnostics answer what happened and what to do. The JSON contract owns
// parser offsets, machine retry codes and the complete diagnostic record.
func conciseError(e map[string]any, inputs []InputHint) cliui.Document {
	details, _ := e["details"].(map[string]any)
	target, _ := details["target"].(map[string]any)
	facts, _ := details["facts"].(map[string]any)
	reason, _ := details["reason"].(string)
	message, _ := e["message"].(string)
	name, _ := target["name"].(string)
	kind, _ := target["kind"].(string)
	phase, _ := details["phase"].(string)
	field, _ := facts["field"].(string)
	var guidance string
	if phase == "parse" {
		for _, input := range inputs {
			if input.Name != field {
				continue
			}
			// Argument and option names can coincide. The parser's sentence
			// identifies which declaration failed, without inspecting raw argv.
			if input.Option != strings.Contains(message, "--"+field) {
				continue
			}
			if reason == "missing_argument" && name != "" {
				if input.Option {
					message = fmt.Sprintf("'%s' requires --%s.", name, field)
				} else {
					message = fmt.Sprintf("'%s' requires a %s argument.", name, field)
				}
			}
			// Handler-level input-combination errors also use parse/invalid_value.
			// Only the parser's pattern failures carry a validation rule; keep
			// semantic guidance such as the configured-command/direct-exec choice.
			_, patternFailure := facts["rule"].(string)
			if reason == "invalid_value" && !input.Option && patternFailure {
				message = fmt.Sprintf("Invalid value for argument '%s'. %s", field, input.Rule)
			}
			if (reason == "missing_argument" || reason == "missing_option_value") && len(input.Choices) > 0 {
				guidance = "Choose " + joinChoices(input.Choices) + "."
			}
			break
		}
	}
	switch reason {
	case "unknown_command":
		if name != "" {
			message = fmt.Sprintf("Unknown command %q.", name)
		}
	case "unknown_option":
		if field, ok := facts["field"].(string); ok && field != "" {
			if !strings.HasPrefix(field, "-") {
				field = "--" + field
			}
			message = fmt.Sprintf("Unknown option %s.", field)
			if name != "" && name != "devtools" {
				message = fmt.Sprintf("Unknown option %s for '%s'.", field, name)
			}
		}
	}
	if name != "" && kind != "" && reason != "unknown_command" && reason != "unknown_option" && !mentionsTarget(message, name) {
		label, known := map[string]string{"command": "", "profile": "Profile ", "variable": "Variable ", "secret": "Secret ", "environment": "Environment ", "process": "Process ", "execution": "Execution "}[kind]
		if !known {
			label = "Resource "
		}
		message = fmt.Sprintf("%s'%s': %s", label, name, message)
	}
	blocks := []cliui.Block{cliui.Paragraph(cliui.Text{Spans: []cliui.Span{
		{Text: "Error: ", Role: cliui.Failure}, {Text: message},
	}, Flow: cliui.Prose})}
	if guidance != "" {
		blocks = append(blocks, cliui.Paragraph(cliui.Words(guidance)))
	}
	suggested, _ := stringArray(facts["suggestions"])
	correction := (reason == "unknown_command" || reason == "unknown_option") && len(suggested) > 0
	if correction {
		choices := make([]string, len(suggested))
		for i, candidate := range suggested {
			if reason == "unknown_option" {
				candidate = "--" + candidate
			}
			choices[i] = fmt.Sprintf("%q", candidate)
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Words("Did you mean "+strings.Join(choices, " or ")+"?")))
	}
	// Scope is useful for a failed operation, but adds nothing to a parse typo.
	if reason != "unknown_command" && reason != "unknown_option" {
		var scope []cliui.Field
		for _, key := range []string{"profile", "env", "directory", "config_path"} {
			if value, ok := target[key].(string); ok && value != "" {
				scope = append(scope, cliui.Field{Label: cliui.Literal(map[string]string{"profile": "Profile", "env": "Environment", "directory": "Directory", "config_path": "Configuration"}[key]), Value: cliui.Literal(value)})
			}
		}
		if len(scope) > 0 {
			blocks = append(blocks, cliui.Fields(scope...))
		}
		blocks = append(blocks, diagnosticLocations(details)...)
		blocks = append(blocks, diagnosticFacts(facts)...)
	}
	blocks = append(blocks, recoveryNotes(details)...)
	if conflicts, ok := facts["import_items"].([]any); ok && len(conflicts) > 0 {
		var names []string
		for _, value := range conflicts {
			if item, ok := value.(map[string]any); ok {
				if key, ok := item["key"].(string); ok && key != "" {
					names = append(names, key)
				}
			}
		}
		if len(names) > 0 {
			blocks = append(blocks, cliui.Paragraph(cliui.Words("Conflicting keys: "+strings.Join(names, ", ")+".")))
		}
	}
	if outcomes, ok := facts["outcomes"].([]any); ok {
		for _, value := range outcomes {
			if outcome, ok := value.(map[string]any); ok {
				name, _ := outcome["command"].(string)
				if name == "" {
					name, _ = outcome["name"].(string)
				}
				state, _ := outcome["state"].(string)
				if name != "" && state != "" {
					blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal(name), Value: cliui.Literal(strings.ReplaceAll(state, "_", " "))}))
				}
			}
		}
	}
	for _, key := range []string{"checks", "causes", "warnings", "issues", "items"} {
		if rows, ok := details[key].([]any); ok && len(rows) > 0 {
			blocks = append(blocks, conciseDiagnostics(rows, false)...)
		}
	}
	if report, ok := details["report"].(map[string]any); ok {
		for _, key := range []string{"checks", "issues", "commands"} {
			if rows, ok := report[key].([]any); ok {
				blocks = append(blocks, conciseDiagnostics(rows, false)...)
			}
		}
	}
	if !correction {
		if actions, valid := actionBlocks(details["remedies"]); valid {
			blocks = append(blocks, actions...)
		}
	}
	return cliui.NewDocument(blocks...)
}

func mentionsTarget(message, name string) bool {
	// A substring such as "env" in "environment" does not identify the target.
	return strings.Contains(" "+message+" ", " "+name+" ") || strings.Contains(message, "'"+name+"'") || strings.Contains(message, fmt.Sprintf("%q", name))
}

func joinChoices(choices []string) string {
	if len(choices) == 1 {
		return choices[0]
	}
	return strings.Join(choices[:len(choices)-1], ", ") + " or " + choices[len(choices)-1]
}

func recoveryNotes(details map[string]any) []cliui.Block {
	var result []cliui.Block
	effects, _ := details["effects"].(string)
	requestID, _ := details["request_id"].(string)
	if requestID == "" {
		if target, ok := details["target"].(map[string]any); ok {
			requestID, _ = target["request_id"].(string)
		}
	}
	switch effects {
	case "committed":
		result = append(result, cliui.Paragraph(cliui.Words("The changes were saved.")))
	case "partial":
		result = append(result, cliui.Paragraph(cliui.Words("Some changes were applied.")))
	case "unknown":
		result = append(result, cliui.Paragraph(cliui.Words("Changes may have been applied. Check the current state before trying again.")))
	}
	if effects == "partial" || effects == "unknown" {
		if requestID != "" && details["retry"] != "same_request" {
			result = append(result, cliui.Fields(cliui.Field{Label: cliui.Literal("Request ID"), Value: cliui.Literal(requestID)}))
		}
	}
	if retry, _ := details["retry"].(string); retry == "same_request" {
		message := "Retry using the same request ID."
		if requestID != "" {
			message = "Retry using the same request ID: " + requestID
		}
		result = append(result, cliui.Paragraph(cliui.Literal(message)))
	} else if retry == "inspect_first" && effects != "unknown" {
		result = append(result, cliui.Paragraph(cliui.Words("Check the current state before trying again.")))
	}
	return result
}

// Diagnostic records are sentences and actionable checks, not recursive dumps.
// Unknown fields remain available through the unchanged public JSON response.
func conciseDiagnostics(rows []any, showPassed bool) []cliui.Block {
	var result []cliui.Block
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			if message, ok := value.(string); ok && message != "" {
				result = append(result, cliui.Paragraph(cliui.Words(message)))
			}
			continue
		}
		status, _ := row["status"].(string)
		if !showPassed && (status == "pass" || status == "not_applicable" || status == "skipped") {
			continue
		}
		message, _ := row["message"].(string)
		if message == "" {
			reason, _ := row["reason"].(string)
			message = map[string]string{"unknown_field": "Unknown configuration field.", "invalid_type": "Invalid value type.", "syntax_error": "Invalid TOML syntax."}[reason]
		}
		if message == "" {
			// Public causes may expose only a code/reason; show a readable label.
			for _, key := range []string{"code", "reason", "name", "id"} {
				if text, ok := row[key].(string); ok && text != "" {
					message = strings.ReplaceAll(text, "_", " ")
					if text == "unknown" {
						message = "The cause could not be determined."
					}
					break
				}
			}
		}
		if message != "" {
			if key, ok := row["key"].(string); ok && key != "" {
				message = key + ": " + message
			} else if section, ok := row["section"].(string); ok && section != "" {
				message = section + ": " + message
			}
			if target, ok := row["target"].(map[string]any); ok {
				if name, ok := target["name"].(string); ok && name != "" && name != row["id"] && !strings.Contains(message, name) {
					message += " (" + name + ")"
				}
			}
			label, role := stateLabel(status)
			if status == "" {
				label = ""
			}
			text := cliui.Text{Flow: cliui.Prose}
			if label != "" {
				text.Spans = append(text.Spans, cliui.Span{Text: label + "  ", Role: role})
			}
			text.Spans = append(text.Spans, cliui.Span{Text: message})
			result = append(result, cliui.Paragraph(text))
		}
		result = append(result, diagnosticLocations(row)...)
		facts, _ := row["facts"].(map[string]any)
		result = append(result, diagnosticFacts(facts)...)
		for _, key := range []string{"checks", "causes", "issues", "warnings"} {
			if nested, ok := row[key].([]any); ok {
				result = append(result, conciseDiagnostics(nested, showPassed)...)
			}
		}
		// Aggregate diagnostics wrap public error details in each subsystem issue.
		// Keep their recovery guidance without exposing the wrapper's metadata.
		if details, ok := row["details"].(map[string]any); ok {
			facts, _ := details["facts"].(map[string]any)
			result = append(result, diagnosticLocations(details)...)
			result = append(result, diagnosticFacts(facts)...)
			result = append(result, recoveryNotes(details)...)
			for _, key := range []string{"checks", "causes", "issues", "warnings"} {
				if nested, ok := details[key].([]any); ok {
					result = append(result, conciseDiagnostics(nested, showPassed)...)
				}
			}
			if actions, valid := actionBlocks(details["remedies"]); valid {
				result = append(result, actions...)
			}
		}
		if actions, valid := actionBlocks(row["remedies"]); valid {
			result = append(result, actions...)
		}
	}
	return result
}

func diagnosticLocations(record map[string]any) []cliui.Block {
	sources, _ := record["sources"].([]any)
	var result []cliui.Block
	for _, value := range sources {
		source, ok := value.(map[string]any)
		if !ok || source["kind"] != "config" && source["kind"] != "dotenv" && source["kind"] != "json" {
			continue
		}
		path, _ := stringArray(source["path"])
		var parts []string
		if len(path) > 0 {
			location := strings.Join(path, ".")
			if source["kind"] == "json" {
				location = ""
				for _, field := range path {
					if _, err := strconv.ParseUint(field, 10, 64); err == nil {
						location += "[" + field + "]"
					} else {
						if location != "" {
							location += "."
						}
						location += field
					}
				}
			}
			parts = append(parts, location)
		}
		if line, ok := source["line"]; ok && fmt.Sprint(line) != "0" {
			parts = append(parts, "line "+fmt.Sprint(line))
			if column, ok := source["column"]; ok && fmt.Sprint(column) != "0" {
				parts = append(parts, "column "+fmt.Sprint(column))
			}
		}
		if len(parts) > 0 {
			result = append(result, cliui.Paragraph(cliui.Literal("At "+strings.Join(parts, ", ")+".")))
		}
	}
	return result
}

// Only correction facts belong in text; parser offsets and unknown metadata
// remain in the public JSON diagnostic.
func diagnosticFacts(facts map[string]any) []cliui.Block {
	var result []cliui.Block
	expected, _ := facts["expected_type"].(string)
	observed, _ := facts["observed_type"].(string)
	if expected != "" && observed != "" {
		result = append(result, cliui.Paragraph(cliui.Words(fmt.Sprintf("Expected %s; received %s.", expected, observed))))
	}
	number := func(key string) (string, bool) {
		n, ok := facts[key].(json.Number)
		if !ok {
			return "", false
		}
		_, err := strconv.ParseUint(string(n), 10, 64)
		return string(n), err == nil
	}
	count, countOK := number("observed")
	limit, limitOK := number("limit")
	if countOK && limitOK {
		kind, _ := facts["limit_kind"].(string)
		label := map[string]string{"patterns": "patterns", "pattern_bytes": "bytes per pattern", "total_pattern_bytes": "total pattern bytes", "states": "matcher states", "outputs_per_state": "outputs per matcher state"}[kind]
		if label == "" {
			if role, _ := facts["input_role"].(string); role != "" {
				label = role + " input bytes"
			}
		}
		if label != "" {
			prefix := "Observed"
			if exact, ok := facts["observed_exact"].(bool); ok && !exact {
				prefix = "Read at least"
			}
			result = append(result, cliui.Paragraph(cliui.Words(fmt.Sprintf("%s %s %s; limit %s.", prefix, count, label, limit))))
		}
	}
	return result
}
