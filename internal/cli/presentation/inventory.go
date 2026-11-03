package presentation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

type inventoryLayout struct {
	columns  []ViewColumn
	optional []int
	group    string
}

func inventoryLayoutFor(d Definition) inventoryLayout {
	l := inventoryLayout{columns: d.Columns}
	switch d.Command {
	case "project status", "process list":
		l.columns = []ViewColumn{{"command", "Command", ""}, {"state", "State", "state"}, {"readiness", "Readiness", "readiness"}, {"id", "Execution ID", ""}}
		if d.Command == "process list" {
			l.group = "project"
		}
	case "variable list", "secret list":
		l.optional = []int{2}
	case "bundle list":
		l.columns = []ViewColumn{{"name", "Bundle", ""}, {"value_ids", "Keys", "count"}, {"bundle_id", "Bundle ID", ""}}
		l.optional = []int{1}
	case "values uses":
		l.columns = append(l.columns, ViewColumn{"selection", "Bundle", ""})
		l.group = "directory"
	case "values execution list":
		l.columns = append(l.columns, ViewColumn{"expires_at", "Expires", ""})
	case "instance list":
		l.columns = []ViewColumn{{"alias", "Alias", "alias"}, {"location_status", "Location", "location"}, {"directory", "Directory", ""}, {"instance_id", "Instance ID", ""}}
		l.optional = []int{0}
		l.group = "profile"
	case "port list":
		l.columns = []ViewColumn{{"name", "Name", ""}, {"port", "Port", ""}}
		l.group = "project"
	case "proxy list":
		l.columns = []ViewColumn{{"host", "Host / Route", "route"}, {"target_port", "Port", ""}, {"listener", "Listener", "listener"}, {"status", "State", "state"}}
		l.group = "proxy"
	case "proxy logs":
		l.columns = append(l.columns, ViewColumn{"reason", "Reason", "reason"})
		l.optional = []int{3}
	case "expose list":
		l.columns[0].Label = "Exposure ID"
	}
	return l
}

func inventoryView(d Definition, data map[string]any) cliui.Document {
	layout := inventoryLayoutFor(d)
	items, _ := data["items"].([]any)
	if d.Command == "process list" {
		selection, _ := data["selection"].(map[string]any)
		if text(selection["view"]) == "current" {
			layout.columns = []ViewColumn{{"command", "Command", ""}, {"current_state", "Current", "state"}, {"last_attempt", "Last attempt", "attempt"}, {"last_changed", "Last changed", ""}}
		} else if text(selection["view"]) == "history" {
			layout.columns = []ViewColumn{{"command", "Command", ""}, {"state", "State", "state"}, {"created_at", "Created", ""}, {"ended_at", "Ended", "ended"}, {"reason", "Reason", "reason"}, {"execution_id", "Execution ID", ""}}
		}
	}
	if selection, ok := data["selection"].(map[string]any); ok {
		// Collection order already reflects the selected query. Regrouping here
		// would put text rows in a different order from JSON and its cursor.
		layout.group = ""
		add := func(key, label string) {
			for _, c := range layout.columns {
				if c.Path == key {
					return
				}
			}
			layout.columns = append(layout.columns, ViewColumn{key, label, ""})
		}
		for _, value := range items {
			row, _ := value.(map[string]any)
			if text(selection["profile"]) == "" && text(row["profile"]) != "" {
				add("profile", "Profile")
			}
			if text(selection["directory"]) == "" && text(row["directory"]) != "" {
				add("directory", "Directory")
			}
		}
	}
	title := listTitle(d.Title, data)
	if d.Command == "port list" {
		title = listTitle("Port assignments", data)
	}
	blocks := []cliui.Block{cliui.Heading(cliui.Literal(title), 1)}
	consumed := map[string]bool{"items": true}
	if text(data["profile"]) != "" {
		consumed["profile"] = true
	}
	blocks = append(blocks, scope(data, consumed)...)
	type group struct {
		name string
		rows []any
	}
	groups := map[string]*group{}
	var missing, issues []any
	for _, value := range items {
		row, _ := value.(map[string]any)
		if layout.group == "proxy" && row["kind"] != "route" {
			if row["status"] == "location_missing" {
				missing = append(missing, value)
			} else {
				issues = append(issues, value)
			}
			continue
		}
		name := ""
		profile, directory := text(row["profile"]), text(row["directory"])
		switch layout.group {
		case "project", "proxy":
			name = profile
			if directory != "" {
				if name != "" {
					name += "  "
				}
				name += directory
			}
			if row["scope"] == "global" {
				name = "Global"
			}
		case "profile":
			name = profile
		case "directory":
			name = directory
		}
		if groups[name] == nil {
			groups[name] = &group{name: name}
		}
		groups[name].rows = append(groups[name].rows, value)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if names[i] == "Global" || names[j] == "Global" {
			return names[i] == "Global" && names[j] != "Global"
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		g := groups[name]
		if layout.group == "proxy" {
			sort.SliceStable(g.rows, func(i, j int) bool {
				return plainCell(inventoryCell(g.rows[i], layout.columns[0])) < plainCell(inventoryCell(g.rows[j], layout.columns[0]))
			})
		}
		table := inventoryTable(layout, g.rows)
		if name != "" {
			blocks = append(blocks, cliui.Section(cliui.Literal(name), table))
		} else {
			blocks = append(blocks, table)
		}
	}
	if len(items) == 0 {
		blocks = append(blocks, cliui.Paragraph(cliui.Words("No "+strings.ToLower(d.Title)+".")))
	}
	if len(missing) > 0 {
		table := inventoryTable(inventoryLayout{columns: []ViewColumn{{"profile", "Profile", ""}, {"directory", "Directory", ""}}}, missing)
		blocks = append(blocks, cliui.Section(cliui.Literal("Missing locations"), table))
	}
	if len(issues) > 0 {
		table := inventoryTable(inventoryLayout{columns: []ViewColumn{{"profile", "Profile", ""}, {"directory", "Directory", ""}, {"status", "State", "state"}}}, issues)
		blocks = append(blocks, cliui.Section(cliui.Literal("Location issues"), table))
	}
	if len(items) > 0 {
		count := fmt.Sprintf("%d results", len(items))
		if len(items) == 1 {
			count = "1 result"
		}
		if d.Command == "port list" {
			count = fmt.Sprintf("%d assignments", len(items))
			if len(items) == 1 {
				count = "1 assignment"
			}
		}
		if d.Command == "proxy list" {
			counts := map[string]int{}
			routes := 0
			for _, item := range items {
				row, _ := item.(map[string]any)
				if row["kind"] == "route" {
					routes++
					counts[text(row["status"])]++
				}
			}
			count = fmt.Sprintf("%d routes", routes)
			var states []string
			for _, status := range []string{"ready", "activation_required", "alias_missing"} {
				if counts[status] > 0 {
					label, _ := inventoryState(status)
					states = append(states, fmt.Sprintf("%d %s", counts[status], strings.ToLower(label)))
					delete(counts, status)
				}
			}
			for _, status := range keysInt(counts) {
				label, _ := inventoryState(status)
				states = append(states, fmt.Sprintf("%d %s", counts[status], strings.ToLower(label)))
			}
			if len(states) > 0 {
				count += ": " + strings.Join(states, ", ")
			}
			count += "."
			if len(missing) > 0 {
				count += fmt.Sprintf("\n%d missing locations.", len(missing))
			}
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Styled(count, cliui.Muted)))
	}
	if d.Command == "bundle list" && text(data["revision"]) != "" {
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Revision"), Value: display(data["revision"])}))
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func keysInt(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func text(value any) string { s, _ := value.(string); return s }
func plainCell(value cliui.Text) string {
	var b strings.Builder
	for _, span := range value.Spans {
		b.WriteString(span.Text)
	}
	return b.String()
}

func inventoryTable(layout inventoryLayout, items []any) cliui.Block {
	columns := make([]cliui.Column, len(layout.columns))
	for i, c := range layout.columns {
		columns[i] = cliui.Column{Label: cliui.Literal(c.Label)}
	}
	rows := make([]cliui.Row, len(items))
	for i, item := range items {
		for _, c := range layout.columns {
			rows[i].Cells = append(rows[i].Cells, inventoryCell(item, c))
		}
	}
	table, _ := cliui.CompactTable(columns, rows, layout.optional...)
	return table
}

func inventoryCell(item any, column ViewColumn) cliui.Text {
	row, _ := item.(map[string]any)
	value := item
	if column.Path != "" {
		value = item
		for _, part := range strings.Split(column.Path, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				value = nil
				break
			}
			value = object[part]
		}
	}
	s := text(value)
	if column.Display == "" && (column.Path == "state" || column.Path == "completion_status" || column.Path == "execution_status" || column.Path == "execution_state" || column.Path == "delivery_state") {
		return domainState(value)
	}
	if column.Path == "env" && s == "" {
		return cliui.Literal("common")
	}
	if column.Path == "overrides" {
		if value == true {
			return cliui.Literal("Yes")
		}
		return cliui.Literal("No")
	}
	if column.Display == "onoff" {
		if value == true {
			return cliui.Literal("Present")
		}
		return cliui.Literal("Absent")
	}
	switch column.Display {
	case "attempt":
		if attempt, ok := value.(map[string]any); ok {
			state, _ := inventoryState(text(attempt["state"]))
			label := state
			if action := text(attempt["action"]); action != "" {
				label = action + ": " + state
			}
			if reason := text(attempt["reason"]); reason != "" {
				label += " (" + reason + ")"
			}
			return cliui.Literal(label)
		}
		return cliui.Literal("Not attempted")
	case "ended":
		if value == nil {
			if row["state"] == "running" || row["state"] == "starting" {
				return cliui.Literal("Not ended")
			}
			return cliui.Literal("Not recorded")
		}
		return display(value)
	case "yesno":
		if value == true {
			return cliui.Literal("Yes")
		}
		return cliui.Literal("No")
	case "check_state":
		label, role := stateLabel(s)
		return cliui.Styled(label, role)
	case "summary":
		if object, ok := value.(map[string]any); ok {
			if summary := text(object["summary"]); summary != "" {
				return cliui.Literal(summary)
			}
			return cliui.Literal("Recorded event")
		}
	case "run":
		if object, ok := value.(map[string]any); ok {
			return display(object["run_id"])
		}
	case "result":
		if object, ok := value.(map[string]any); ok {
			if result := text(object["result"]); result != "" {
				label := "Recorded " + result
				if object["applicable_when_recorded"] == false {
					label += " (not applicable)"
				}
				return cliui.Literal(label)
			}
		}
		if value == nil || s == "" {
			if _, exists := row[column.Path]; !exists {
				return cliui.Styled("Unavailable", cliui.Warning)
			}
			return cliui.Literal("Not recorded")
		}
	case "count":
		if items, ok := value.([]any); ok {
			return cliui.Literal(fmt.Sprint(len(items)))
		}
	case "alias":
		if s == "" {
			s = "Unnamed"
		}
		return cliui.Literal(s)
	case "listener":
		if s == "" {
			s = "default"
		}
		return cliui.Literal(s)
	case "route":
		if s == "" {
			s = "route:" + text(row["proxy"])
			if s == "route:" {
				s = "route:" + text(row["route_id"])
			}
		}
		return cliui.Literal(s)
	case "state":
		label, role := inventoryState(s)
		return cliui.Styled(label, role)
	case "location":
		label, ok := map[string]string{"available": "Available", "present": "Available", "missing": "Missing", "unknown": "Unverified"}[s]
		if !ok {
			label = "Unverified"
		}
		return cliui.Literal(label)
	case "readiness":
		if row["ready_configured"] == false {
			return cliui.Literal("Not configured")
		}
		if ready, ok := value.(map[string]any); ok {
			if ready["ready"] == true {
				return cliui.Styled("Ready", cliui.Success)
			}
			if ready["reason"] == "not_configured" {
				return cliui.Literal("Not configured")
			}
			return cliui.Styled("Not ready", cliui.Failure)
		}
		return cliui.Styled("Unverified", cliui.Warning)
	case "reason":
		if s == "" {
			return cliui.Literal("None")
		}
	}
	if value == nil {
		return cliui.Styled("Unavailable", cliui.Warning)
	}
	return display(value)
}

func domainState(value any) cliui.Text {
	state := text(value)
	if state == "preparing" || state == "auth_required" {
		label, role := inventoryState(state)
		return cliui.Styled(label, role)
	}
	protection := map[string]string{"uninitialized": "Uninitialized", "locked": "Locked", "unlocked": "Unlocked", "locking": "Locking", "locked_manual": "Manually locked", "recovery_required": "Recovery required", "released": "Released", "revoked": "Revoked", "taken_over": "Taken over", "interrupted": "Interrupted"}
	if label, ok := protection[state]; ok {
		role := cliui.Primary
		if state == "locking" {
			role = cliui.Info
		}
		if state == "recovery_required" {
			role = cliui.Warning
		}
		return cliui.Styled(label, role)
	}
	labels := map[string]string{"open": "Open", "done": "Done", "canceled": "Canceled", "active": "Active", "inactive": "Inactive", "draft": "Draft", "none": "None", "current": "Current", "stale": "Stale", "idle": "Idle", "blocked": "Blocked", "ready": "Ready", "running": "Running", "starting": "Starting", "stopped": "Stopped", "failed": "Failed", "unknown": "Unknown", "prepared": "Prepared", "approved": "Approved", "delivering": "Delivering", "delivered": "Delivered", "expired": "Expired", "not_started": "Not started", "unreported": "Unreported", "reported_started": "Reported started", "reported_finished": "Reported finished", "reported_failed": "Reported failed", "reported_unknown": "Reported unknown", "finished": "Finished", "completed": "Completed", "needs_work": "Needs work"}
	label, ok := labels[state]
	if !ok {
		if value == nil {
			return cliui.Styled("Unavailable", cliui.Warning)
		}
		return cliui.Styled("Unrecognized: "+state, cliui.Warning)
	}
	role := cliui.Primary
	switch state {
	case "unknown", "stale", "blocked", "expired", "needs_work":
		role = cliui.Warning
	case "failed", "reported_failed":
		role = cliui.Failure
	case "running", "starting", "delivering":
		role = cliui.Info
	}
	return cliui.Styled(label, role)
}

func inventoryState(state string) (string, cliui.Role) {
	switch state {
	case "preparing":
		return "Preparing", cliui.Info
	case "active":
		return "Active", cliui.Info
	case "auth_required":
		return "Authentication required", cliui.Warning
	case "recovery_required":
		return "Recovery required", cliui.Warning
	}
	labels := map[string]string{"exited": "Exited", "not_started": "Not started", "conflict": "Conflicting runs", "ready": "Ready", "pass": "Ready", "fail": "Not ready", "not_ready": "Not ready", "running": "Running", "starting": "Starting", "stopped": "Stopped", "failed": "Failed", "interrupted": "Interrupted", "unknown": "Unknown", "activation_required": "Needs activation", "alias_missing": "Alias missing", "location_missing": "Location missing", "assignment_missing": "No assignment", "host_conflict": "Host conflict", "config_unavailable": "Configuration unavailable", "instance_conflict": "Instance conflict", "host_invalid": "Invalid host", "disabled": "Disabled", "pending": "Pending", "expired": "Expired", "canceled": "Canceled"}
	label, ok := labels[state]
	if !ok {
		return "Unrecognized: " + state, cliui.Warning
	}
	role := cliui.Primary
	switch state {
	case "ready", "pass":
		role = cliui.Success
	case "failed", "fail", "not_ready":
		role = cliui.Failure
	case "running", "starting":
		role = cliui.Info
	case "unknown", "activation_required", "alias_missing", "location_missing", "assignment_missing", "host_conflict", "config_unavailable", "instance_conflict", "host_invalid", "pending", "expired":
		role = cliui.Warning
	}
	return label, role
}

func inventoryNotes(data map[string]any) []cliui.Block {
	var blocks []cliui.Block
	if selection, ok := data["selection"].(map[string]any); ok {
		fields := []cliui.Field{}
		if view := text(selection["view"]); view != "" {
			fields = append(fields, cliui.Field{Label: cliui.Literal("View"), Value: cliui.Literal(view)})
		}
		if chosen, ok := selection["scope"].(map[string]any); ok {
			for _, key := range []string{"kind", "profile", "directory", "instance_id"} {
				if value := text(chosen[key]); value != "" {
					fields = append(fields, cliui.Field{Label: cliui.Literal(title(key)), Value: cliui.Literal(value)})
				}
			}
		}
		if filters, ok := selection["filters"].(map[string]any); ok {
			for _, key := range []string{"query", "sort", "order", "time_field", "since", "until"} {
				if value := text(filters[key]); value != "" {
					fields = append(fields, cliui.Field{Label: cliui.Literal(title(key)), Value: cliui.Literal(value)})
				}
			}
			if values, ok := filters["filters"].(map[string]any); ok {
				names := []string{}
				for key := range values {
					names = append(names, key)
				}
				sort.Strings(names)
				for _, key := range names {
					if v, ok := stringArray(values[key]); ok {
						fields = append(fields, cliui.Field{Label: cliui.Literal(title(key)), Value: cliui.Literal(strings.Join(v, ", "))})
					}
				}
			}
		}
		if len(fields) > 0 {
			blocks = append(blocks, cliui.Fields(fields...))
		}
	}
	if page, ok := data["page"].(map[string]any); ok {
		message := fmt.Sprintf("Showing %v results on this page.", page["count"])
		if page["has_more"] == true {
			message += " More results are available."
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Styled(message, cliui.Muted)))
	}
	for _, key := range []string{"issues", "warnings", "unresolved", "pending"} {
		if rows, ok := data[key].([]any); ok {
			blocks = append(blocks, conciseDiagnostics(rows, false)...)
		}
	}
	if actions, valid := actionBlocks(data["next_actions"]); valid {
		blocks = append(blocks, actions...)
	}
	if cursor, ok := data["next_cursor"].(string); ok && cursor != "" {
		if actions, _ := data["next_actions"].([]any); len(actions) == 0 {
			blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Next cursor"), Value: cliui.Literal(cursor)}))
		}
	}
	return blocks
}
