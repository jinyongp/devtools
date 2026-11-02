package presentation

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	if e["code"] == "config_format_required" && report != nil {
		return cliui.NewDocument(append([]cliui.Block{errorLine(text(e["message"]))}, formattingReviewBlocks(report)...)...), true
	}
	if len(causes) == 0 && facts["owner"] == nil && facts["related_items"] == nil && report == nil && details["reason"] != "output_exists" {
		return cliui.Document{}, false
	}
	target, _ := details["target"].(map[string]any)
	var roots []map[string]any
	for _, value := range causes {
		cause, _ := value.(map[string]any)
		if cause == nil {
			continue
		}
		if text(cause["message"]) == "" {
			message := map[string]string{"unknown_field": "Unknown configuration field.", "invalid_type": "Invalid value type.", "syntax_error": "Invalid TOML syntax."}[text(cause["reason"])]
			if message == "" {
				message = strings.ReplaceAll(text(cause["code"]), "_", " ")
			}
			cause["message"] = message
		}
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
	if details["reason"] == "output_exists" {
		if target["kind"] == "file" {
			blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Output"), Value: display(target["name"])}))
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("The existing file was preserved.")))
	}
	if len(roots) == 0 {
		blocks = append(blocks, causalLocations(details, target)...)
		blocks = append(blocks, diagnosticFacts(facts)...)
	}
	if report != nil {
		switch {
		case report["scope"] == "storage":
			blocks = append(blocks, storageReportBlocks(report)...)
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
		covered := map[string]bool{}
		for _, cause := range roots {
			key, _ := json.Marshal([]any{cause["code"], cause["target"], cause["facts"]})
			covered[string(key)] = true
		}
		var remaining []any
		for _, value := range checks {
			check, _ := value.(map[string]any)
			key, _ := json.Marshal([]any{check["reason"], check["target"], check["facts"]})
			if !covered[string(key)] {
				remaining = append(remaining, value)
			}
		}
		blocks = append(blocks, unverifiedChecks(remaining)...)
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
		if pair[0] == "bundle" && (value == "" || value == "all" && target["_display_bundle_explicit"] != true) || pair[0] == "mode" && value == "foreground" && !foreground {
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
	if d.Command == "diagnostics" {
		return aggregateDiagnosisView(data)
	}
	if d.Command == "doctor" {
		if data["scope"] == "storage" {
			return cliui.NewDocument(storageReportBlocks(data)...)
		}
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
	role := cliui.Primary
	switch state {
	case "Ready", "Valid", "Available":
		role = cliui.Success
	case "Not ready", "Invalid":
		role = cliui.Failure
	case "Unverified", "Unavailable":
		role = cliui.Warning
	}
	blocks := []cliui.Block{stateHeading(d.Title, state, role)}
	blocks = append(blocks, scope(data, map[string]bool{})...)
	if d.Command == "config check" {
		if path := text(data["config_path"]); path != "" {
			blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Directory"), Value: cliui.Literal(filepath.Dir(path))}, cliui.Field{Label: cliui.Literal("Input"), Value: cliui.Literal(path)}))
		}
	}
	blocks = append(blocks, selectedFields(data, "id", "name", "port"))
	for _, key := range []string{"checks"} {
		if rows, ok := data[key].([]any); ok {
			blocks = append(blocks, conciseDiagnostics(rows, false)...)
		}
	}
	if readiness, ok := data["readiness"].(map[string]any); ok {
		if readiness["reason"] == "not_configured" {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("No readiness check is configured.")))
		} else if readiness["ready"] == false {
			message := map[string]string{"probe_timeout": "The readiness check timed out.", "probe_execution_failed": "The readiness check could not start.", "probe_failed": "The readiness check failed."}[text(readiness["reason"])]
			if message == "" {
				message = "The configured readiness condition has not been satisfied."
			}
			blocks = append(blocks, cliui.Paragraph(cliui.Words(message)))
			if code, exists := readiness["exit_code"]; exists && code != nil && fmt.Sprint(code) != "0" {
				blocks = append(blocks, cliui.Paragraph(cliui.Literal("Readiness check exit code: "+fmt.Sprint(code))))
			}
		}
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func doctorReportBlocks(data map[string]any, heading bool) []cliui.Block {
	var blocks []cliui.Block
	if heading {
		state, role := stateLabel(text(data["state"]))
		blocks = append(blocks, stateHeading("Doctor", state, role))
	}
	target := map[string]any{}
	for _, key := range []string{"profile", "directory", "_display_bundle_explicit"} {
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
		if bundle, ok := command["selection"].(string); ok {
			target["bundle"] = bundle
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
	affected := map[string][]string{}
	checkIdentity := func(check map[string]any) string {
		identity, _ := json.Marshal([]any{check["reason"], check["target"], check["facts"], check["sources"], check["status"], check["message"], check["blocked_by"], check["diagnostic"]})
		return string(identity)
	}
	if checks, ok := data["checks"].([]any); ok {
		failed = append(failed, checks...)
	}
	for _, value := range commands {
		command, _ := value.(map[string]any)
		if checks, ok := command["checks"].([]any); ok {
			failed = append(failed, checks...)
			for _, value := range checks {
				check, _ := value.(map[string]any)
				if check["status"] == "fail" {
					id := checkIdentity(check)
					name := text(command["name"])
					if names := affected[id]; len(names) == 0 || names[len(names)-1] != name {
						affected[id] = append(names, name)
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, value := range failed {
		check, _ := value.(map[string]any)
		if check["status"] == "unknown" || check["status"] == "fail" {
			if remedies, ok := check["remedies"].([]any); ok {
				actions = append(actions, remedies...)
			}
		}
		if check["status"] != "fail" {
			continue
		}
		facts, _ := check["facts"].(map[string]any)
		identity := checkIdentity(check)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		message := text(check["message"])
		if target, ok := check["target"].(map[string]any); ok {
			if name := text(target["name"]); name != "" && !mentionsTarget(message, name) {
				message = name + ": " + message
			}
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Words(message)))
		blocks = append(blocks, observationDiagnosticBlocks(check["diagnostic"])...)
		if names := affected[identity]; len(commands) > 1 && len(names) > 0 {
			blocks = append(blocks, cliui.Paragraph(cliui.Words("Commands: "+strings.Join(names, ", ")+".")))
		}
		blocks = append(blocks, resourceFacts(facts, text(check["message"]))...)
		blocks = append(blocks, causalLocations(check, map[string]any{"config_path": data["config_path"]})...)
	}
	blocks = append(blocks, unverifiedChecks(failed)...)
	blocks = append(blocks, recoveryNotes(data)...)
	blocks = append(blocks, inventoryNotes(data)...)
	if next, ok := actionBlocks(actions); ok {
		blocks = append(blocks, next...)
	}
	return blocks
}
