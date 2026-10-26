package presentation

import (
	"fmt"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

// Text diagnostics answer what happened and what to do. The JSON contract owns
// parser offsets, machine retry codes and the complete diagnostic record.
func conciseError(e map[string]any) cliui.Document {
	details, _ := e["details"].(map[string]any)
	target, _ := details["target"].(map[string]any)
	facts, _ := details["facts"].(map[string]any)
	reason, _ := details["reason"].(string)
	message, _ := e["message"].(string)
	name, _ := target["name"].(string)
	switch reason {
	case "unknown_command":
		if name != "" {
			message = fmt.Sprintf("Unknown command %q.", name)
		}
	case "unknown_option":
		if field, ok := facts["field"].(string); ok && field != "" {
			message = fmt.Sprintf("Unknown option --%s.", field)
		}
	}
	blocks := []cliui.Block{cliui.Paragraph(cliui.Text{Spans: []cliui.Span{
		{Text: "Error: ", Role: cliui.Failure}, {Text: message},
	}, Flow: cliui.Prose})}
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
	if !correction && name != "" && !strings.Contains(message, name) && reason != "unknown_command" && reason != "unknown_option" {
		kind, _ := target["kind"].(string)
		label := map[string]string{"command": "Command", "profile": "Profile", "variable": "Variable", "secret": "Secret", "environment": "Environment", "process": "Process", "execution": "Execution"}[kind]
		if label == "" {
			label = "Resource"
		}
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal(label), Value: cliui.Literal(name)}))
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
		for _, key := range []string{"checks", "causes", "issues", "warnings"} {
			if nested, ok := row[key].([]any); ok {
				result = append(result, conciseDiagnostics(nested, showPassed)...)
			}
		}
		if actions, valid := actionBlocks(row["remedies"]); valid {
			result = append(result, actions...)
		}
	}
	return result
}
