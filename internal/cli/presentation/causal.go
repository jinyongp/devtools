package presentation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func causalErrorView(e map[string]any) (cliui.Document, bool) {
	details, _ := e["details"].(map[string]any)
	if details["phase"] == "parse" {
		return cliui.Document{}, false
	}
	causes, _ := details["causes"].([]any)
	facts, _ := details["facts"].(map[string]any)
	report, _ := details["report"].(map[string]any)
	if len(causes) == 0 && facts["owner"] == nil && facts["related_items"] == nil && report == nil {
		return cliui.Document{}, false
	}
	target, _ := details["target"].(map[string]any)
	var roots []map[string]any
	for _, value := range causes {
		cause, _ := value.(map[string]any)
		parents, _ := cause["caused_by"].([]any)
		if len(parents) == 0 {
			roots = append(roots, cause)
		}
	}
	if primary, ok := details["primary_cause_id"].(string); ok {
		for i, cause := range roots {
			if cause["id"] == primary && i > 0 {
				roots = append([]map[string]any{cause}, append(roots[:i], roots[i+1:]...)...)
				break
			}
		}
	}
	first := []cliui.Block{errorLine(text(e["message"]))}
	if len(roots) == 1 && text(roots[0]["message"]) != "" && roots[0]["message"] != e["message"] {
		first = append(first, cliui.Paragraph(cliui.Words(text(roots[0]["message"]))))
	}
	blocks := []cliui.Block{cliui.Stack(first...)}
	blocks = append(blocks, errorScope(target)...)
	if len(roots) == 1 {
		root := roots[0]
		rootFacts, _ := root["facts"].(map[string]any)
		blocks = append(blocks, resourceFacts(rootFacts, text(root["message"]))...)
		blocks = append(blocks, causalLocations(root, target)...)
		blocks = append(blocks, diagnosticFacts(rootFacts)...)
	} else if len(roots) > 1 {
		for i, root := range roots {
			children := []cliui.Block{cliui.Paragraph(cliui.Words(fmt.Sprintf("%d. %s", i+1, text(root["message"]))))}
			rootFacts, _ := root["facts"].(map[string]any)
			children = append(children, resourceFacts(rootFacts, text(root["message"]))...)
			children = append(children, causalLocations(root, target)...)
			children = append(children, diagnosticFacts(rootFacts)...)
			blocks = append(blocks, cliui.Stack(children...))
		}
	}
	blocks = append(blocks, resourceFacts(facts, text(e["message"]))...)
	if len(roots) == 0 {
		blocks = append(blocks, causalLocations(details, target)...)
		blocks = append(blocks, diagnosticFacts(facts)...)
	}
	if report != nil {
		switch {
		case report["changes"] != nil:
			if path := text(report["config_path"]); path != "" {
				blocks = append(blocks, selectedFields(report, "config_path"))
			}
			if rows, ok := report["changes"].([]any); ok && len(rows) > 0 {
				blocks = append(blocks, changeTable(rows))
			}
			if report["dry_run"] == true {
				blocks = append(blocks, cliui.Paragraph(cliui.Literal("No files were changed.")))
			}
		case report["commands"] != nil:
			blocks = append(blocks, doctorReportBlocks(report, false)...)
		default:
			if rows, ok := report["checks"].([]any); ok {
				blocks = append(blocks, conciseDiagnostics(rows, false)...)
			}
			if rows, ok := report["issues"].([]any); ok {
				blocks = append(blocks, conciseDiagnostics(rows, false)...)
			}
		}
	}
	if facts["replayed"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Saved result replayed.")))
	}
	blocks = append(blocks, recoveryNotes(details)...)
	blocks = append(blocks, launchRecoveryNotes(e, details, facts)...)
	if checks, ok := details["checks"].([]any); ok {
		blocks = append(blocks, unverifiedChecks(checks)...)
	}
	var actions []any
	if values, ok := details["remedies"].([]any); ok {
		actions = append(actions, values...)
	}
	for _, root := range roots {
		if values, ok := root["remedies"].([]any); ok {
			actions = append(actions, values...)
		}
	}
	if rendered, ok := actionBlocks(actions); ok {
		blocks = append(blocks, rendered...)
	}
	return cliui.NewDocument(blocks...), true
}

func errorLine(message string) cliui.Block {
	return cliui.Paragraph(cliui.Text{Flow: cliui.Prose, Spans: []cliui.Span{{Text: "Error: ", Role: cliui.Failure}, {Text: message}}})
}

func errorScope(target map[string]any) []cliui.Block {
	return errorScopeWithMode(target, false)
}

func errorScopeWithMode(target map[string]any, foreground bool) []cliui.Block {
	var fields []cliui.Field
	for _, pair := range [][2]string{{"profile", "Profile"}, {"directory", "Directory"}, {"env", "Environment"}, {"bundle", "Bundle"}, {"mode", "Mode"}} {
		value, exists := target[pair[0]].(string)
		if !exists {
			continue
		}
		if pair[0] == "bundle" && (value == "" || value == "all") || pair[0] == "mode" && value == "foreground" && !foreground {
			continue
		}
		if pair[0] == "env" && value == "" {
			value = "common"
		}
		if value != "" {
			fields = append(fields, cliui.Field{Label: cliui.Literal(pair[1]), Value: cliui.Literal(value)})
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return []cliui.Block{cliui.Fields(fields...)}
}

func resourceFacts(facts map[string]any, message string) []cliui.Block {
	var fields []cliui.Field
	add := func(label string, value any) {
		if value != nil && value != "" {
			fields = append(fields, cliui.Field{Label: cliui.Literal(label), Value: display(value)})
		}
	}
	if service := text(facts["service"]); service != "" && !mentionsTarget(message, service) {
		add("Service", service)
	}
	if port, exists := facts["port"]; exists {
		observation, _ := facts["port_observation"].(map[string]any)
		value := "Unavailable: the service assignment could not be read."
		if port != nil {
			condition := "TCP not checked"
			switch observation["status"] {
			case "free":
				condition = "no TCP listener"
			case "in_use":
				condition = "TCP in use"
			}
			value = fmt.Sprintf("%v (%s)", port, condition)
		}
		add("Port", value)
	}
	owner, _ := facts["owner"].(map[string]any)
	if owner != nil {
		value := "Unavailable: " + ownerReason(text(owner["reason"]))
		if owner["status"] == "verified" {
			name := text(owner["command"])
			if name == "" {
				name = "Controller"
			}
			kind := text(owner["kind"])
			if kind == "managed" && owner["execution_id"] != nil {
				value = name + " (managed)"
			} else {
				value = fmt.Sprintf("%s (%s, PID %v)", name, kind, owner["pid"])
			}
		}
		add("Owner", value)
	}
	add("Previous execution", facts["previous_id"])
	if owner["status"] == "verified" {
		add("Execution", owner["execution_id"])
	}
	add("Executable", facts["executable"])
	add("Expected", facts["expected_version"])
	add("Observed", facts["observed_version"])
	if facts["expected_revision"] != nil {
		add("Expected", facts["expected_revision"])
		add("Observed", facts["observed_revision"])
	}
	add("State", facts["state"])
	var blocks []cliui.Block
	if len(fields) > 0 {
		blocks = append(blocks, cliui.Fields(fields...))
	}
	if observation, ok := facts["port_observation"].(map[string]any); ok && observation["status"] == "unavailable" && facts["port"] != nil {
		blocks = append(blocks, cliui.Paragraph(cliui.Words("Observation incomplete: TCP could not be checked within the available permissions or deadline.")))
	}
	if items, ok := facts["related_items"].([]any); ok && len(items) > 0 {
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"id", "ID", ""}, {"state", "Lifecycle", ""}, {"completion_status", "Completion", ""}, {"execution_status", "Execution", ""}}}, items))
		for _, value := range items {
			item, _ := value.(map[string]any)
			if blockers, ok := item["blockers"].([]any); ok {
				blocks = append(blocks, conciseDiagnostics(blockers, false)...)
			}
		}
	}
	return blocks
}

func ownerReason(reason string) string {
	if description := map[string]string{"legacy_run": "the existing run has no ownership metadata.", "metadata_unavailable": "ownership metadata could not be read or verified.", "permission_denied": "permission to inspect the owner was denied.", "state_changed": "the run changed during observation.", "deadline_exceeded": "the owner observation deadline elapsed.", "unsupported_owner_query": "the owner cannot be verified by this observer."}[reason]; description != "" {
		return description
	}
	return "the owner could not be verified."
}

func causalLocations(record, target map[string]any) []cliui.Block {
	if record["code"] == "port_run_active" || record["code"] == "port_in_use" {
		return nil
	}
	if text(target["config_path"]) == "" {
		return diagnosticLocations(record)
	}
	sources, _ := record["sources"].([]any)
	var blocks []cliui.Block
	for _, value := range sources {
		source, _ := value.(map[string]any)
		if source["kind"] != "config" {
			continue
		}
		path, _ := stringArray(source["path"])
		location := text(target["config_path"])
		if source["line"] != nil {
			location += fmt.Sprint(":", source["line"])
			if source["column"] != nil {
				location += fmt.Sprint(":", source["column"])
			}
		}
		if len(path) > 0 {
			location += " (" + strings.Join(path, ".") + ")"
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("At "+location+".")))
	}
	return blocks
}

func unverifiedChecks(checks []any) []cliui.Block {
	groups := map[string][]string{}
	var order []string
	seen := map[string]bool{}
	for _, value := range checks {
		check, _ := value.(map[string]any)
		if check["status"] != "unknown" {
			continue
		}
		facts, _ := check["facts"].(map[string]any)
		target, _ := check["target"].(map[string]any)
		name := text(facts["executable"])
		if name == "" {
			name = text(target["name"])
		}
		if name == "" {
			name = text(check["id"])
		}
		identity, _ := json.Marshal([]any{name, facts["expected_version"], facts["search_origin"], target["profile"], target["env"], check["blocked_by"]})
		if seen[string(identity)] {
			continue
		}
		seen[string(identity)] = true
		if expected := text(facts["expected_version"]); expected != "" {
			name += " (expected " + expected + ")"
		}
		reason := text(check["reason"])
		if _, ok := groups[reason]; !ok {
			order = append(order, reason)
		}
		groups[reason] = append(groups[reason], name)
	}
	var blocks []cliui.Block
	for _, reason := range order {
		explanation := map[string]string{"binding_unresolved": "their search environment depends on an unresolved binding", "environment_unresolved": "value authentication did not complete", "secret_search_environment": "their search PATH contains protected values", "deadline_exceeded": "the diagnosis deadline elapsed", "canceled": "diagnosis was canceled"}[reason]
		if explanation == "" {
			explanation = "the required observation could not complete"
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Words("Unverified checks: "+strings.Join(groups[reason], ", ")+" ("+explanation+").")))
	}
	return blocks
}

func diagnosisPurposeView(d Definition, data map[string]any) cliui.Document {
	if d.Command == "doctor" {
		return cliui.NewDocument(doctorReportBlocks(data, true)...)
	}
	state := "Unverified"
	if data["ready"] == true {
		state = "Ready"
	} else if data["ready"] == false {
		state = "Not ready"
	}
	if data["valid"] == true {
		state = "Valid"
	} else if data["valid"] == false {
		state = "Invalid"
	}
	if data["available"] == true {
		state = "Available"
	} else if data["available"] == false {
		state = "Unavailable"
	}
	if readiness, ok := data["readiness"].(map[string]any); ok {
		if readiness["ready"] == true {
			state = "Ready"
		} else if readiness["ready"] == false {
			state = "Not ready"
		}
	}
	if data["ready_configured"] == false {
		state = "Not configured"
	}
	blocks := []cliui.Block{cliui.Heading(cliui.Literal(d.Title+": "+state+"."), 1)}
	blocks = append(blocks, scope(data, map[string]bool{})...)
	blocks = append(blocks, selectedFields(data, "id", "port"))
	for _, key := range []string{"checks"} {
		if rows, ok := data[key].([]any); ok {
			blocks = append(blocks, conciseDiagnostics(rows, false)...)
		}
	}
	if readiness, ok := data["readiness"].(map[string]any); ok {
		if readiness["reason"] == "not_configured" {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("No readiness check is configured.")))
		} else if readiness["ready"] == false {
			blocks = append(blocks, cliui.Paragraph(cliui.Words("The configured readiness condition has not been satisfied.")))
		}
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func doctorReportBlocks(data map[string]any, heading bool) []cliui.Block {
	var blocks []cliui.Block
	if heading {
		state, _ := stateLabel(text(data["state"]))
		blocks = append(blocks, cliui.Heading(cliui.Literal("Doctor: "+state+"."), 1))
	}
	target := map[string]any{}
	for _, key := range []string{"profile", "directory"} {
		if value, ok := data[key]; ok {
			target[key] = value
		}
	}
	commands, _ := data["commands"].([]any)
	if len(commands) == 1 {
		command, _ := commands[0].(map[string]any)
		if env, ok := command["env"].(string); ok {
			target["env"] = env
		}
	}
	if mode := text(data["mode"]); mode != "" {
		target["mode"] = mode
	}
	blocks = append(blocks, errorScopeWithMode(target, true)...)
	blocks = []cliui.Block{cliui.Stack(blocks...)}
	if len(commands) > 0 {
		columns := []ViewColumn{{"name", "Command", ""}, {"state", "Status", "check_state"}}
		if len(commands) > 1 {
			columns = []ViewColumn{{"name", "Command", ""}, {"env", "Environment", ""}, {"state", "Status", "check_state"}}
		}
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: columns}, commands))
	}
	var failed, actions []any
	if checks, ok := data["checks"].([]any); ok {
		failed = append(failed, checks...)
	}
	for _, value := range commands {
		command, _ := value.(map[string]any)
		if checks, ok := command["checks"].([]any); ok {
			failed = append(failed, checks...)
		}
	}
	seen := map[string]bool{}
	for _, value := range failed {
		check, _ := value.(map[string]any)
		if check["status"] != "fail" {
			continue
		}
		facts, _ := check["facts"].(map[string]any)
		identity, _ := json.Marshal([]any{check["reason"], check["target"], facts})
		if seen[string(identity)] {
			continue
		}
		seen[string(identity)] = true
		blocks = append(blocks, cliui.Paragraph(cliui.Words(text(check["message"]))))
		blocks = append(blocks, resourceFacts(facts, text(check["message"]))...)
		blocks = append(blocks, causalLocations(check, map[string]any{"config_path": data["config_path"]})...)
		if remedies, ok := check["remedies"].([]any); ok {
			actions = append(actions, remedies...)
		}
	}
	blocks = append(blocks, unverifiedChecks(failed)...)
	if next, ok := actionBlocks(actions); ok {
		blocks = append(blocks, next...)
	}
	return blocks
}
