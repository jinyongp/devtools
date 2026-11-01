package presentation

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func purposeView(d Definition, data map[string]any) (cliui.Document, bool) {
	if d.Command == "" {
		return cliui.Document{}, false
	}
	if document, handled := variantView(d, data); handled {
		return document, true
	}
	switch d.Command {
	case "config fmt":
		if data["dry_run"] == true {
			return cliui.NewDocument(append([]cliui.Block{outcomeHeading("Preview: Format configuration.")}, formattingReviewBlocks(data)...)...), true
		}
	case "profile diff":
		return comparisonView(data), true
	case "values runner address":
		return cliui.NewDocument(cliui.Heading(cliui.Literal("Runner address"), 1), selectedFields(data, "runner_id", "instance_id", "endpoint", "protocol", "redaction_protocol")), true
	case "proxy ca export":
		item, _ := data["item"].(map[string]any)
		blocks := []cliui.Block{cliui.Heading(cliui.Literal("Public certificate"), 1), selectedFields(item, "path", "fingerprint")}
		if certificate, ok := item["certificate"].(string); ok {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal(strings.TrimSuffix(certificate, "\n"))))
		}
		return cliui.NewDocument(blocks...), true
	case "cleanup archives":
		items, _ := data["items"].([]any)
		copy := map[string]any{"items": items}
		for _, v := range items {
			row, _ := v.(map[string]any)
			row["archive_state"] = "Archived"
			if row["restored_at"] != nil {
				row["archive_state"] = "Restored"
			}
			if row["purged_at"] != nil {
				row["archive_state"] = "Purged"
			}
		}
		return inventoryView(Definition{Command: d.Command, Title: "Cleanup archives", Columns: []ViewColumn{{"id", "Archive ID", ""}, {"kind", "Kind", ""}, {"source", "Target", ""}, {"archive_state", "State", ""}, {"expires", "Expires", ""}}}, copy), true
	}
	switch d.Family {
	case "task":
		return taskPurposeView(d, data), true
	case "mutation", "preview":
		return mutationPurposeView(d, data), true
	case "diagnostic":
		return diagnosisPurposeView(d, data), true
	}
	return cliui.Document{}, false
}

func selectedFields(data map[string]any, names ...string) cliui.Block {
	var fields []cliui.Field
	labels := map[string]string{"id": "ID", "runner_id": "Runner", "instance_id": "Instance", "endpoint": "Endpoint", "path": "Output", "config_path": "Input", "protocol_version": "Protocol", "completion_status": "Completion", "execution_status": "Execution", "state": "State", "title": "Title", "request_id": "Request ID", "record_id": "Record ID", "basis_id": "Basis ID", "candidate_digest": "Candidate digest", "apply_digest": "Apply digest", "server_id": "Server ID", "url": "URL", "not_after": "Expires", "selection": "Bundle"}
	for _, key := range names {
		value, exists := data[key]
		if !exists || value == nil || value == "" {
			continue
		}
		label := labels[key]
		if label == "" {
			label = title(key)
		}
		if scalar(value) {
			displayed := display(value)
			if key == "state" || key == "completion_status" || key == "execution_status" || key == "delivery_state" || key == "execution_state" {
				displayed = domainState(value)
			}
			fields = append(fields, cliui.Field{Label: cliui.Literal(label), Value: displayed})
		}
	}
	return cliui.Fields(fields...)
}

func operationPhrase(command string) string {
	phrases := map[string]string{"config fmt": "Format configuration", "config edit": "Edit configuration", "config migrate": "Migrate configuration", "backup restore": "Restore backup", "profile import": "Import profile", "proxy teardown": "Remove proxy setup"}
	if phrase := phrases[command]; phrase != "" {
		return phrase
	}
	words := strings.Fields(command)
	if len(words) == 0 {
		return "Operation"
	}
	verbs := map[string]string{"add": "Add", "create": "Create", "set": "Set", "update": "Update", "remove": "Remove", "rename": "Rename", "configure": "Configure", "start": "Start", "stop": "Stop", "restart": "Restart", "up": "Activate", "down": "Deactivate", "clear": "Clear", "rm": "Remove", "unset": "Unset", "init": "Initialize", "prepare": "Prepare", "approve": "Approve", "deliver": "Deliver inputs", "report": "Record result", "register": "Register", "unregister": "Unregister", "generate": "Generate", "apply": "Apply", "restore": "Restore", "purge": "Purge", "allocate": "Allocate", "release": "Release", "prune": "Prune"}
	last := words[len(words)-1]
	verb := verbs[last]
	if verb == "" {
		return title(command)
	}
	return verb + " " + strings.Join(words[:len(words)-1], " ")
}

func mutationPurposeView(d Definition, data map[string]any) cliui.Document {
	preview := d.Family == "preview" || boolField(data, "dry_run")
	phrase := operationPhrase(d.Command)
	message := completedSentence(d)
	item, _ := data["item"].(map[string]any)
	if changed, exists := data["changed"].(bool); exists && !changed {
		message = "No changes to " + operationResource(d.Command) + "."
	}
	if preview {
		message = "Preview: " + phrase
		if d.Target != "" {
			message += fmt.Sprintf(" %q", d.Target)
		}
		if d.Destination != "" {
			message += fmt.Sprintf(" to %q", d.Destination)
		}
		message += "."
	}
	if d.Command == "dashboard" {
		message = "Dashboard is stopped."
		if item["running"] == true {
			message = "Dashboard is running."
		}
	}
	if d.Command == "expose start" && item["id"] == nil {
		message = "Local route URL."
	}
	if d.Command == "expose start" && item["id"] != nil && data["changed"] == false {
		message = "Reused exposure."
	}
	if item["state"] == "starting" && (strings.HasSuffix(d.Command, " start") || strings.HasSuffix(d.Command, " restart") || strings.HasSuffix(d.Command, " up")) {
		message = "Start requested for " + operationResource(d.Command) + "."
	}
	if strings.HasPrefix(d.Command, "values execution ") {
		state := text(item["delivery_state"])
		switch d.Command {
		case "values execution prepare":
			message = "Prepared value inputs."
		case "values execution approve":
			message = "Recorded input approval."
		case "values execution deliver":
			message = "Input delivery: " + state + "."
		case "values execution report":
			message = "Recorded execution result."
		case "values execution cancel":
			message = "Recorded execution cancellation."
		}
	}
	if boolField(data, "replayed") {
		message = "Recorded result: " + message
	}
	blocks := []cliui.Block{outcomeHeading(message)}
	scopeData := map[string]any{}
	for _, key := range []string{"profile", "directory", "env", "selection", "mode"} {
		if value, ok := data[key]; ok {
			scopeData[key] = value
		} else if value, ok := item[key]; ok && key != "env" && (key != "directory" || detailLocationScope(d.Command)) {
			scopeData[key] = value
		}
	}
	if item["inject"] == true {
		if value, ok := item["env"]; ok {
			scopeData["env"] = value
		}
	}
	if path := text(data["config_path"]); !preview && path != "" && scopeData["directory"] == nil {
		scopeData["directory"] = filepath.Dir(path)
	}
	blocks = append(blocks, scope(scopeData, map[string]bool{})...)
	if d.Command == "bundle set" && d.MembersKnown {
		if len(d.MemberNames) == 0 {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("No bundle members selected.")))
		} else {
			members := make([]any, 0, len(d.MemberNames))
			seen := map[string]bool{}
			for _, name := range d.MemberNames {
				if !seen[name] {
					seen[name] = true
					members = append(members, name)
				}
			}
			blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"", "Key", ""}}}, members))
		}
	}
	if preview && text(data["config_path"]) != "" {
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Directory"), Value: cliui.Literal(filepath.Dir(text(data["config_path"])))}, cliui.Field{Label: cliui.Literal("Input"), Value: display(data["config_path"])}))
	}
	if item != nil {
		blocks = append(blocks, mutationItemFacts(item)...)
		if strings.HasPrefix(d.Command, "backup ") {
			blocks = append(blocks, selectedFields(item, "directory"))
		}
		if strings.HasPrefix(d.Command, "values execution ") {
			blocks = append(blocks, selectedFields(item, "revision"))
		}
	}
	blocks = append(blocks, selectedFields(data, "id", "execution_id", "path", "output", "archive_id", "plan_id", "record_id", "basis_id", "recipient"))
	for _, key := range []string{"removed", "retained", "archives", "targets", "outcomes"} {
		if rows, ok := data[key].([]any); ok && len(rows) > 0 {
			if key == "targets" {
				first, _ := rows[0].(map[string]any)
				if first["profile"] != nil {
					targets := make([]any, 0, len(rows))
					for _, value := range rows {
						row, _ := value.(map[string]any)
						state := "Unavailable"
						if exists, ok := row["exists"].(bool); ok {
							state = "New"
							if exists {
								state = "Exists"
							}
						}
						targets = append(targets, map[string]any{"source": row["source"], "target": row["profile"], "state": state})
					}
					blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"source", "Source", ""}, {"target", "Target", ""}, {"state", "State", "literal"}}}, targets))
					continue
				}
			}
			blocks = append(blocks, cliui.Section(cliui.Literal(title(key)), batchTable(rows)))
		}
	}
	if d.Command == "profile import" {
		if diff, ok := data["diff"].(map[string]any); ok {
			blocks = append(blocks, comparisonView(diff).Blocks()...)
		}
	}
	if rows, ok := data["changes"].([]any); ok {
		if len(rows) == 0 && preview {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("No proposed changes.")))
		} else if len(rows) > 0 {
			blocks = append(blocks, changeTable(rows))
		}
	}
	if impact, ok := data["impact"].(map[string]any); ok && d.Command != "config fmt" {
		if names, ok := impact["commands"].([]any); ok && len(names) > 0 {
			blocks = append(blocks, cliui.Section(cliui.Literal(fmt.Sprintf("Affected commands (%d)", len(names))), inventoryTable(inventoryLayout{columns: []ViewColumn{{"", "Command", ""}}}, names)))
		}
		if impact["running_processes"] == "not_checked" || impact["external_usage"] == "not_checked" {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("Running process and external usage impact was not checked.")))
		}
	}
	if rows, ok := data["items"].([]any); ok && len(rows) > 0 {
		blocks = append(blocks, batchTable(rows))
	}
	if preview {
		blocks = append(blocks, selectedFields(data, "candidate_digest", "revision", "apply_digest", "digest"))
		if item["digest"] != nil {
			blocks = append(blocks, selectedFields(item, "digest"))
		}
		note := "No changes were applied."
		if strings.HasPrefix(d.Command, "config ") || strings.HasPrefix(d.Command, "command ") {
			note = "No files were changed."
		}
		if strings.HasPrefix(d.Command, "bundle ") {
			note = "No values were changed."
		}
		if d.Command == "proxy teardown" {
			note = "No routes were changed."
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Literal(note)))
	}
	if boolField(data, "replayed") {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Saved result replayed.")))
	}
	blocks = append(blocks, recoveryNotes(data)...)
	if data["target_exists"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("The target profile already exists.")))
	}
	if data["applicable"] == false && preview {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("This preview cannot be applied until its conflicts are resolved.")))
	}
	blocks = append(blocks, selectedFields(data, "safety_backup", "expires"))
	if profiles, ok := data["profiles"].([]any); ok && len(profiles) > 0 {
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"profile", "Profile", ""}, {"values", "Values", "onoff"}, {"tasks", "Tasks", "onoff"}}}, profiles))
	}
	if item["auth"] != nil {
		blocks = append(blocks, named("Authentication", item["auth"])...)
	}
	if item["client_setup"] != nil {
		blocks = append(blocks, named("Client setup", item["client_setup"])...)
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func operationResource(command string) string {
	switch command {
	case "task checkpoint", "task release":
		return "run"
	case "init":
		return "project configuration"
	case "import":
		return "values"
	case "dashboard", "dashboard stop":
		return "dashboard"
	case "profile transfer prepare":
		return "profile transfer identity"
	case "backup keygen":
		return "backup keys"
	case "proxy up", "proxy down":
		return "routes"
	}
	for _, entry := range [][2]string{{"config tool ", "tool declaration"}, {"config preset ", "preset declaration"}, {"config ", "configuration"}, {"command ", "command declaration"}, {"task workstream ", "workstream"}, {"task validation ", "validation"}, {"task ", "task"}, {"values execution ", "value execution"}, {"values runner ", "runner"}, {"variable ", "variable"}, {"secret ", "secret"}, {"env ", "environment"}, {"bundle ", "bundle"}, {"proxy ca ", "certificate authority"}, {"proxy hosts ", "hosts entries"}, {"proxy ", "proxy"}, {"process ", "process"}, {"project ", "project"}, {"profile ", "profile"}, {"backup ", "backup"}, {"cleanup ", "cleanup state"}, {"instance ", "instance"}, {"port ", "port assignment"}, {"expose ", "exposure"}, {"vault ", "vault"}} {
		if strings.HasPrefix(command, entry[0]) {
			return entry[1]
		}
	}
	return strings.ToLower(title(command))
}

func completedSentence(d Definition) string {
	parts := strings.Fields(d.Command)
	last := parts[len(parts)-1]
	verbs := map[string]string{"init": "Initialized", "create": "Created", "add": "Created", "set": "Updated", "update": "Updated", "unset": "Removed", "remove": "Removed", "rm": "Removed", "rename": "Renamed", "name": "Named", "move": "Moved", "configure": "Configured", "start": "Started", "stop": "Stopped", "restart": "Restarted", "up": "Activated", "down": "Deactivated", "clear": "Cleared", "generate": "Generated", "allocate": "Allocated", "release": "Released", "prune": "Pruned", "purge": "Purged", "restore": "Restored", "export": "Exported", "import": "Imported", "prepare": "Prepared", "approve": "Approved", "deliver": "Delivered inputs to", "report": "Recorded result for", "register": "Registered", "unregister": "Unregistered", "lock": "Locked", "unlock": "Unlocked", "recover": "Recovered", "rewrap": "Changed protection for", "fmt": "Formatted", "edit": "Updated", "migrate": "Migrated", "claim": "Claimed", "resume": "Resumed", "takeover": "Took over", "unclaim": "Released", "checkpoint": "Recorded checkpoint for", "done": "Completed", "hold": "Held", "unhold": "Unheld", "cancel": "Canceled", "reopen": "Reopened", "activate": "Activated", "close": "Closed", "record": "Recorded result for", "waive": "Waived requirement for", "unwaive": "Restored requirement for", "basis": "Created validation basis for", "accept": "Accepted evidence for", "sync": "Synchronized", "attach": "Attached", "detach": "Detached"}
	verb := verbs[last]
	if verb == "" {
		return title(d.Command) + " completed."
	}
	message := verb + " " + operationResource(d.Command)
	if d.Target != "" {
		message += fmt.Sprintf(" %q", d.Target)
	}
	if d.Destination != "" {
		message += fmt.Sprintf(" to %q", d.Destination)
	}
	return message + "."
}

func mutationItemFacts(item map[string]any) []cliui.Block {
	ordered := []string{"previous_id", "id", "name", "alias", "key", "command", "state", "status", "delivery_state", "execution_state", "host", "listener", "url", "endpoint", "port", "origin_port", "serve_port", "path", "output", "fingerprint", "not_after", "runner_id", "instance_id", "execution_id", "expires_at", "idle_expires_at", "identity_file", "recipient_file", "recipient", "backend", "provider", "pid", "exit_code", "reason"}
	blocks := []cliui.Block{selectedFields(item, ordered...)}
	if _, exists := item["ready_configured"]; exists {
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Readiness"), Value: inventoryCell(item, ViewColumn{Path: "readiness", Display: "readiness"})}))
	}
	if item["requires_explicit_unlock"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Explicit unlock is required.")))
	}
	if item["client_restart_may_be_required"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Affected clients may need to restart to observe the trust change.")))
	}
	return blocks
}

func changeTable(rows []any) cliui.Block {
	columns := []cliui.Column{{Label: cliui.Literal("Action")}, {Label: cliui.Literal("Target")}}
	rename := false
	for _, value := range rows {
		row, _ := value.(map[string]any)
		if path, _ := row["to_path"].([]any); len(path) > 0 {
			rename = true
		}
	}
	if rename {
		columns = append(columns, cliui.Column{Label: cliui.Literal("Destination")})
	}
	var output []cliui.Row
	for _, value := range rows {
		row, _ := value.(map[string]any)
		op := text(row["op"])
		path, _ := stringArray(row["path"])
		action := map[string]string{"add": "Add", "update": "Update", "remove": "Remove", "rename": "Rename", "format": "Format"}[op]
		if action == "" {
			action = op
		}
		target := strings.Join(path, ".")
		if target == "" {
			target = "Configuration declarations"
		}
		cells := []cliui.Text{cliui.Literal(action), cliui.Literal(target)}
		if rename {
			destination, _ := stringArray(row["to_path"])
			cells = append(cells, cliui.Literal(strings.Join(destination, ".")))
		}
		output = append(output, cliui.Row{Cells: cells})
	}
	table, _ := cliui.Table(columns, output)
	return table
}

func batchTable(items []any) cliui.Block {
	if len(items) > 0 {
		first, _ := items[0].(map[string]any)
		if first["key"] != nil && first["kind"] != nil {
			return inventoryTable(inventoryLayout{columns: []ViewColumn{{"kind", "Kind", ""}, {"key", "Key", ""}, {"action", "Action", ""}}}, items)
		}
	}
	var rows []cliui.Row
	for _, value := range items {
		item, _ := value.(map[string]any)
		if name, ok := value.(string); ok {
			rows = append(rows, cliui.Row{Cells: []cliui.Text{cliui.Literal(name), cliui.Literal("Selected")}})
			continue
		}
		target := text(item["command"])
		if target == "" {
			target = text(item["name"])
		}
		if target == "" {
			target = text(item["id"])
		}
		state := text(item["status"])
		if state == "" {
			state = text(item["state"])
		}
		if state == "" {
			state = text(item["action"])
		}
		if target == "" {
			target = "Unavailable"
		}
		if state == "" {
			state = "Unavailable"
		}
		rows = append(rows, cliui.Row{Cells: []cliui.Text{cliui.Literal(target), cliui.Literal(state)}})
	}
	table, _ := cliui.Table([]cliui.Column{{Label: cliui.Literal("Target")}, {Label: cliui.Literal("Result")}}, rows)
	return table
}

func comparisonView(data map[string]any) cliui.Document {
	message := "Profiles differ."
	if data["different"] == false {
		message = "No differences."
	}
	blocks := []cliui.Block{outcomeHeading(message), selectedFields(data, "left", "right")}
	if data["different"] == false {
		return cliui.NewDocument(blocks...)
	}
	var rows []cliui.Row
	for _, kind := range []string{"envs", "variables", "secrets", "items", "instances"} {
		items, _ := data[kind].([]any)
		for _, value := range items {
			item, _ := value.(map[string]any)
			key := text(item["key"])
			if key == "" {
				key = text(item["name"])
			}
			if key == "" {
				key = text(item["id"])
			}
			left, right := comparisonSide(item["left"], kind), comparisonSide(item["right"], kind)
			if kind == "instances" {
				key = right
				if item["right"] == nil {
					key = left
				}
			}
			if kind == "envs" {
				left, right = "Present", "Present"
				if item["action"] == "added" {
					left = "Absent"
				}
				if item["action"] == "removed" {
					right = "Absent"
				}
			}
			change := map[string]string{"added": "Added", "removed": "Removed", "changed": "Different"}[text(item["action"])]
			if change == "" {
				change = "Unrecognized: " + text(item["action"])
			}
			if item["action"] == "changed" && left != right {
				change += ": " + left + " to " + right
			}
			label := map[string]string{"envs": "Environment", "variables": "Variable", "secrets": "Secret", "items": "Item", "instances": "Instance"}[kind]
			if kind == "items" && text(item["kind"]) != "" {
				label = title(text(item["kind"]))
			}
			rows = append(rows, cliui.Row{Cells: []cliui.Text{cliui.Literal(label), cliui.Literal(key), cliui.Literal(change)}})
		}
	}
	if len(rows) > 0 {
		table, _ := cliui.Table([]cliui.Column{{Label: cliui.Literal("Kind")}, {Label: cliui.Literal("Key")}, {Label: cliui.Literal("Change")}}, rows)
		blocks = append(blocks, table)
		count := fmt.Sprintf("%d differences", len(rows))
		if len(rows) == 1 {
			count = "1 difference"
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Styled(count, cliui.Muted)))
	}
	return cliui.NewDocument(blocks...)
}

func taskPurposeView(d Definition, data map[string]any) cliui.Document {
	if strings.HasSuffix(d.Command, " export") {
		return generic(envelope{ok: true, value: data})
	}
	read := false
	for _, suffix := range []string{" list", " show", " context", " current", " next", " history", " tree", " impact", " check"} {
		read = read || strings.HasSuffix(d.Command, suffix)
	}
	if !read {
		return taskMutationView(d, data)
	}
	if (d.Command == "task next" || d.Command == "task current") && data["item"] == nil {
		items, _ := data["items"].([]any)
		if len(items) == 0 {
			message := "No ready task"
			if d.Command == "task current" {
				message = "No active run"
			}
			if profile := text(data["profile"]); profile != "" {
				message += " for " + profile
			}
			return cliui.NewDocument(cliui.Paragraph(cliui.Literal(message + ".")))
		}
	}
	heading := d.Title
	if strings.HasSuffix(d.Command, "spec show") || strings.HasSuffix(d.Command, "plan show") {
		heading = "Specification"
		if strings.HasSuffix(d.Command, "plan show") {
			heading = "Plan"
		}
		if workstream := text(data["title"]); workstream != "" {
			heading += " for " + workstream
		}
	}
	blocks := []cliui.Block{cliui.Heading(cliui.Literal(heading), 1)}
	blocks = append(blocks, scope(data, map[string]bool{})...)
	if strings.HasSuffix(d.Command, "spec show") || strings.HasSuffix(d.Command, "plan show") {
		item, _ := data["item"].(map[string]any)
		if body, ok := item["body"].(string); ok {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal(body)))
		}
		for _, key := range []string{"requirements", "acceptance"} {
			if rows, ok := item[key].([]any); ok && len(rows) > 0 {
				blocks = append(blocks, cliui.Section(cliui.Literal(title(key)), inventoryTable(inventoryLayout{columns: []ViewColumn{{"key", "Key", ""}, {"text", "Text", ""}}}, rows)))
			}
		}
		if refs, ok := data["task_order"].([]any); ok && len(refs) > 0 {
			names := map[string]string{}
			members, _ := data["tasks"].([]any)
			for _, value := range members {
				task, _ := value.(map[string]any)
				names[text(task["id"])] = text(task["title"])
			}
			rows := make([]any, 0, len(refs))
			for i, ref := range refs {
				name := names[text(ref)]
				if name == "" {
					name = "Unavailable"
				}
				rows = append(rows, map[string]any{"order": i + 1, "title": name, "id": ref})
			}
			blocks = append(blocks, cliui.Section(cliui.Literal("Task order"), inventoryTable(inventoryLayout{columns: []ViewColumn{{"order", "#", ""}, {"title", "Task", ""}, {"id", "ID", ""}}}, rows)))
		}
		if refs, ok := item["validation_ids"].([]any); ok && len(refs) > 0 {
			names := map[string]string{}
			members, _ := data["validations"].([]any)
			for _, value := range members {
				validation, _ := value.(map[string]any)
				names[text(validation["id"])] = text(validation["title"])
			}
			rows := make([]any, 0, len(refs))
			for _, ref := range refs {
				name := names[text(ref)]
				if name == "" {
					name = "Unavailable"
				}
				rows = append(rows, map[string]any{"title": name, "id": ref})
			}
			blocks = append(blocks, cliui.Section(cliui.Literal("Validation references"), inventoryTable(inventoryLayout{columns: []ViewColumn{{"title", "Validation", ""}, {"id", "ID", ""}}}, rows)))
		}
		return cliui.NewDocument(blocks...)
	}
	if d.Command == "task workstream check" {
		var fields []cliui.Field
		for _, pair := range [][2]string{{"valid", "Structure"}, {"execution_ready", "Execution"}, {"closable", "Closure"}} {
			label := "Not ready"
			if data[pair[0]] == true {
				label = "Ready"
			}
			if pair[0] == "valid" {
				label = "Invalid"
				if data[pair[0]] == true {
					label = "Valid"
				}
			}
			fields = append(fields, cliui.Field{Label: cliui.Literal(pair[1]), Value: cliui.Literal(label)})
		}
		blocks = append(blocks, cliui.Fields(fields...))
	}
	item, _ := data["item"].(map[string]any)
	if item != nil {
		blocks = append(blocks, taskIdentity(item)...)
		if description, ok := item["description"].(string); ok && description != "" {
			blocks = append(blocks, cliui.Section(cliui.Literal("Description"), cliui.Paragraph(cliui.Literal(description))))
		}
		if method, ok := item["method"].(string); ok && method != "" {
			blocks = append(blocks, cliui.Section(cliui.Literal("Method"), cliui.Paragraph(cliui.Literal(method))), selectedFields(item, "required", "latest_result"))
		}
		if d.Command == "task validation show" {
			blocks = append(blocks, selectedFields(item, "required", "task_id", "workstream_id"))
		}
	}
	if d.Command == "task validation show" {
		if records, ok := data["records"].([]any); ok && len(records) > 0 {
			blocks = append(blocks, cliui.Section(cliui.Literal("Recorded results"), inventoryTable(inventoryLayout{columns: []ViewColumn{{"record_id", "Record ID", ""}, {"basis_id", "Basis ID", ""}, {"result", "Result", ""}, {"summary", "Summary", ""}}}, records)))
		}
	}
	if strings.HasSuffix(d.Command, " next") && item == nil {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("No ready task.")))
	}
	for _, key := range []string{"items", "tasks", "validations", "nodes", "history"} {
		items, ok := data[key].([]any)
		if !ok {
			continue
		}
		if key == "tasks" && item != nil {
			filtered := []any{}
			for _, value := range items {
				row, _ := value.(map[string]any)
				if row["id"] != item["id"] {
					filtered = append(filtered, value)
				}
			}
			items = filtered
		}
		if len(items) == 0 {
			if key == "items" {
				blocks = append(blocks, cliui.Paragraph(cliui.Literal("No results.")))
			}
			continue
		}
		columns := []ViewColumn{{"title", "Title", ""}, {"state", "Lifecycle", ""}, {"completion_status", "Completion", ""}, {"execution_status", "Execution", ""}, {"id", "ID", ""}}
		switch {
		case key == "history" || strings.HasSuffix(d.Command, " history"):
			columns = []ViewColumn{{"occurred_at", "Time", ""}, {"action", "Action", ""}, {"target_id", "Target", ""}, {"data", "Summary", "summary"}}
		case d.Command == "task current":
			columns = []ViewColumn{{"task_id", "Task", ""}, {"id", "Run", ""}, {"directory", "Directory", ""}, {"state", "State", ""}}
		case d.Command == "task checkpoint list":
			columns = []ViewColumn{{"occurred_at", "Time", ""}, {"data", "Summary", "summary"}, {"data", "Run", "run"}}
		case key == "validations" || d.Command == "task validation list":
			columns = []ViewColumn{{"title", "Title", ""}, {"latest_result", "Result", "result"}, {"required", "Required", "yesno"}, {"id", "ID", ""}}
		case d.Command == "task workstream list":
			columns = []ViewColumn{{"title", "Title", ""}, {"state", "State", ""}, {"completion_status", "Completion", ""}, {"id", "ID", ""}}
		}
		blocks = append(blocks, cliui.Section(cliui.Literal(title(key)), inventoryTable(inventoryLayout{columns: columns}, items)))
		if key == "items" {
			blocks = append(blocks, cliui.Paragraph(cliui.Styled(fmt.Sprintf("%d results", len(items)), cliui.Muted)))
		}
	}
	if strings.HasSuffix(d.Command, " context") {
		if docs, ok := data["documents"].(map[string]any); ok {
			for _, key := range []string{"spec", "plan"} {
				if document, ok := docs[key].(map[string]any); ok {
					if body, ok := document["body"].(string); ok && body != "" {
						blocks = append(blocks, cliui.Section(cliui.Literal(title(key)), cliui.Paragraph(cliui.Literal(body))))
					}
				}
			}
		}
	}
	if edges, ok := data["edges"].([]any); ok && len(edges) > 0 {
		blocks = append(blocks, cliui.Section(cliui.Literal("Dependencies"), inventoryTable(inventoryLayout{columns: []ViewColumn{{"from", "From", ""}, {"to", "To", ""}}}, edges)))
	}
	if data["truncated"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("This response is incomplete; use the scoped continuation to read the remaining items.")))
	}
	if omitted, ok := data["omitted_ids"].([]any); ok && len(omitted) > 0 {
		blocks = append(blocks, cliui.Section(cliui.Literal("Not included"), inventoryTable(inventoryLayout{columns: []ViewColumn{{"", "ID", ""}}}, omitted)))
	}
	if blockers, ok := data["blockers"].([]any); ok {
		blocks = append(blocks, conciseDiagnostics(blockers, false)...)
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func taskIdentity(item map[string]any) []cliui.Block {
	var values []cliui.Field
	idLabel := "ID"
	if kind, ok := item["kind"].(string); ok && kind != "" {
		idLabel = title(kind) + " ID"
	}
	for _, pair := range [][2]string{{"id", idLabel}, {"title", "Title"}, {"state", "Lifecycle"}, {"completion_status", "Completion"}, {"execution_status", "Execution"}} {
		if value, ok := item[pair[0]]; ok && value != nil {
			displayed := display(value)
			if pair[0] != "id" && pair[0] != "title" {
				displayed = domainState(value)
			}
			values = append(values, cliui.Field{Label: cliui.Literal(pair[1]), Value: displayed})
		}
	}
	fields := cliui.Fields(values...)
	blocks := []cliui.Block{fields}
	if blockers, ok := item["blockers"].([]any); ok {
		blocks = append(blocks, conciseDiagnostics(blockers, false)...)
	}
	if dependencies, ok := item["depends_on"].([]any); ok && len(dependencies) > 0 {
		blocks = append(blocks, cliui.Section(cliui.Literal("Dependencies"), inventoryTable(inventoryLayout{columns: []ViewColumn{{"", "ID", ""}}}, dependencies)))
	}
	return blocks
}

func taskMutationView(d Definition, data map[string]any) cliui.Document {
	if data["claimed"] == false {
		message := "No ready task"
		if profile := text(data["profile"]); profile != "" {
			message += " for " + profile
		}
		blocks := []cliui.Block{cliui.Paragraph(cliui.Literal(message + "."))}
		blocks = append(blocks, inventoryNotes(data)...)
		return cliui.NewDocument(blocks...)
	}
	if item, ok := data["item"].(map[string]any); ok {
		d.Target = text(item["title"])
	}
	message := completedSentence(d)
	if data["changed"] == false {
		message = "No changes to " + operationResource(d.Command) + "."
	}
	if data["dry_run"] == true {
		message = "Preview: " + operationPhrase(d.Command) + "."
	}
	if data["claimed"] == false {
		message = "No ready task."
	}
	if d.Command == "task validation record" {
		message = "Recorded validation result."
		if item, ok := data["item"].(map[string]any); ok {
			if latest, ok := item["latest_result"].(map[string]any); ok && text(latest["result"]) != "" {
				message = "Recorded validation result: " + text(latest["result"]) + "."
			}
		}
	}
	if d.Command == "task validation waive" {
		message = "Waived validation requirement."
	}
	if data["replayed"] == true {
		message = "Recorded result: " + message
	}
	blocks := []cliui.Block{outcomeHeading(message)}
	blocks = append(blocks, scope(data, map[string]bool{})...)
	if item, ok := data["item"].(map[string]any); ok {
		if d.Command == "task add" {
			blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Task ID"), Value: display(item["id"])}), selectedFields(item, "workstream_id"))
			if blockers, ok := item["blockers"].([]any); ok {
				blocks = append(blocks, conciseDiagnostics(blockers, false)...)
			}
		} else {
			copy := map[string]any{}
			for key, value := range item {
				copy[key] = value
			}
			if d.Target != "" && strings.Contains(message, fmt.Sprintf("%q", d.Target)) {
				delete(copy, "title")
			}
			blocks = append(blocks, taskIdentity(copy)...)
		}
	}
	if run, ok := data["run"].(map[string]any); ok {
		blocks = append(blocks, cliui.Section(cliui.Literal("Run"), selectedFields(run, "id", "directory", "state")))
	}
	blocks = append(blocks, selectedFields(data, "record_id", "basis_id"))
	if count := data["affected_count"]; count != nil && fmt.Sprint(count) != "0" && fmt.Sprint(count) != "1" {
		blocks = append(blocks, selectedFields(data, "affected_count"))
	}
	if data["applicable"] == false {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("The recorded evidence does not apply to the current definition.")))
	}
	if data["context_valid"] == false && d.Command != "task done" && d.Command != "task release" {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("The execution context is not valid for the current definition.")))
	}
	if refs, ok := data["created_refs"].(map[string]any); ok && len(refs) > 0 {
		var rows []any
		for _, key := range keys(refs) {
			rows = append(rows, map[string]any{"ref": key, "id": refs[key]})
		}
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"ref", "Reference", ""}, {"id", "ID", ""}}}, rows))
	}
	if boolField(data, "replayed") {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Saved result replayed.")))
	}
	if data["dry_run"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("No task state was changed.")))
	}
	if credential, ok := data["context"].(string); ok && credential != "" && data["claimed"] == true {
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Execution context"), Value: cliui.Literal(credential)}))
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}
