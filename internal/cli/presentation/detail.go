package presentation

import (
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func detailLocationScope(command string) bool {
	for _, prefix := range []string{"command ", "project ", "process ", "port ", "instance "} {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}

func detailFieldOrder(d Definition, prefix string) []string {
	fields := detailFieldMembership[d.Command][prefix]
	if (d.Command == "process status" || d.Command == "process start" || d.Command == "process stop" || d.Command == "process restart") && prefix == "item" {
		fields = "command mode state control cleanup ready_configured readiness previous_id id instance_id started_at ended_at exit_code reason capture_logs value_apply value_watch"
	}
	priority := "command name key exec mode state control cleanup status completion_status execution_status ready_configured readiness previous_id id instance_id profile directory env selection inject serve bind requirements ready output provenance config_path unresolved source integrity_verified runner_id execution_id input_id approval delivery_state execution_state expires_at exit_code result reason"
	switch {
	case strings.HasPrefix(d.Command, "vault "):
		priority = "state backend idle_expires_at rewrap"
	case strings.HasPrefix(d.Command, "proxy ") || strings.HasPrefix(d.Command, "expose "):
		priority = "host route_ref listener url route_url port origin_port serve_port state status provider reason auth client_setup trusted present fingerprint not_after"
	case strings.HasPrefix(d.Command, "backup ") || strings.HasPrefix(d.Command, "profile "):
		priority = "profile configured directory created_at profiles values tasks envs variables secrets instances processes task_counts active_process_count"
	case strings.HasPrefix(d.Command, "values "):
		priority = "selection integrity_verified values runner_id runner_instance_id instance_id execution_id external_execution_id input_id delivery_id delivery_state execution_state expires_at revision result exit_code reason external_ref fact_source reporter_instance_id enabled protocol redaction_protocol"
	}
	positions := map[string]int{}
	for i, key := range strings.Fields(priority) {
		positions[key] = i + 1
	}
	keys := strings.Fields(fields)
	sort.SliceStable(keys, func(i, j int) bool {
		a, b := positions[keys[i]], positions[keys[j]]
		if a == 0 {
			a = len(positions) + 1
		}
		if b == 0 {
			b = len(positions) + 1
		}
		if a != b {
			return a < b
		}
		return keys[i] < keys[j]
	})
	return keys
}

func detailRemainder(data map[string]any, consumed map[string]bool, prefix string, d Definition) []cliui.Block {
	var blocks []cliui.Block
	var fields []cliui.Field
	flush := func() {
		if len(fields) > 0 {
			blocks = append(blocks, cliui.Fields(fields...))
			fields = nil
		}
	}
	for _, key := range detailFieldOrder(d, prefix) {
		value, exists := data[key]
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if !exists || consumed[key] || value == nil || value == "" || omit(path, value, d) {
			continue
		}
		if key == "vault_id" || key == "generation" {
			continue
		}
		if prefix == "item" && (key == "profile" || key == "directory" && detailLocationScope(d.Command) || key == "env" || key == "selection") {
			continue
		}
		label := title(key)
		if key == "exec" {
			if argv, ok := stringArray(value); ok {
				fields = append(fields, cliui.Field{Label: cliui.Literal("Exec"), Value: cliui.Styled(argvDisplay(argv), cliui.Muted)})
			}
			continue
		}
		if scalar(value) {
			displayed := display(value)
			if key == "state" || key == "delivery_state" || key == "execution_state" {
				displayed = domainState(value)
			}
			fields = append(fields, cliui.Field{Label: cliui.Literal(label), Value: displayed})
			continue
		}
		if array, ok := value.([]any); ok && len(array) == 0 {
			continue
		}
		if values, ok := stringArray(value); ok && len(values) > 0 {
			flush()
			rows := make([]cliui.Row, 0, len(values))
			for _, value := range values {
				rows = append(rows, cliui.Row{Cells: []cliui.Text{cliui.Literal(value)}})
			}
			table, _ := cliui.Table([]cliui.Column{{Label: cliui.Literal(label)}}, rows)
			if key == "serve" {
				table, _ = cliui.Table([]cliui.Column{{Label: cliui.Literal("Service")}}, rows)
				blocks = append(blocks, cliui.Section(cliui.Literal("Services"), table))
			} else {
				blocks = append(blocks, table)
			}
			continue
		}
		if object, ok := value.(map[string]any); ok {
			if key == "requirements" || key == "output" || key == "readiness" || key == "control" || key == "cleanup" {
				allowed := strings.Fields(map[string]string{"requirements": "tools vars secs", "output": "mode", "readiness": "ready reason exit_code", "control": "state reason stop restart check", "cleanup": "scope state reason"}[key])
				filtered := map[string]any{}
				for _, name := range allowed {
					if value, ok := object[name]; ok {
						filtered[name] = value
					}
				}
				object = filtered
				value = filtered
			}
			if len(object) == 0 {
				continue
			}
		}
		flush()
		sectionLabel := map[string]string{"serve": "Services", "bind": "Bindings", "ready": "Readiness", "output": "Output policy", "provenance": "Provenance", "unresolved": "Unresolved"}[key]
		if sectionLabel == "" {
			sectionLabel = title(key)
		}
		blocks = append(blocks, named(sectionLabel, value)...)
	}
	flush()
	return blocks
}
