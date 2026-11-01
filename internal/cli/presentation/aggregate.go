package presentation

import (
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func aggregateDiagnosisView(data map[string]any) cliui.Document {
	var rows []cliui.Row
	failed, unverified := false, false
	issues, _ := data["issues"].([]any)
	issueSections := map[string]bool{}
	for _, value := range issues {
		issue, _ := value.(map[string]any)
		issueSections[text(issue["section"])] = true
	}
	for _, key := range []string{"platform", "vault", "profiles", "ports", "processes", "proxy", "dashboard", "backup", "network", "external_executions"} {
		section, ok := data[key].(map[string]any)
		if !ok {
			continue
		}
		status := text(section["status"])
		if key == "platform" && text(section["os"]) != "" {
			status = "pass"
			if text(section["storage_filesystem"]) == "unknown" {
				status = "unknown"
			}
		}
		if key == "external_executions" {
			switch section["verification"] {
			case "verified":
				status = "pass"
			case "verification_pending":
				status = "unknown"
			}
		}
		if issueSections[key] {
			status = "fail"
		}
		if status == "" {
			continue
		}
		label, role := stateLabel(status)
		failed = failed || status == "fail" || status == "not_ready"
		unverified = unverified || status != "pass" && status != "ready" && status != "fail" && status != "not_ready" && status != "not_applicable"
		if status == "pass" {
			switch key {
			case "proxy":
				if state := text(section["state"]); state != "" {
					label, role = inventoryState(state)
				}
			case "dashboard":
				label, role = "Stopped", cliui.Primary
				if section["running"] == true {
					label, role = "Running", cliui.Info
				}
			case "backup":
				if section["configured"] == false {
					label, role = "Not configured", cliui.Primary
				}
			}
		}
		rows = append(rows, cliui.Row{Cells: []cliui.Text{cliui.Literal(title(key)), cliui.Styled(label, role)}})
	}
	state := "Ready"
	if failed || !unverified && data["ready"] == false {
		state = "Not ready"
	} else if unverified {
		state = "Unverified"
	}
	blocks := []cliui.Block{cliui.Heading(cliui.Literal("Diagnostics: "+state+"."), 1)}
	if len(rows) > 0 {
		table, _ := cliui.Table([]cliui.Column{{Label: cliui.Literal("Subsystem")}, {Label: cliui.Literal("State")}}, rows)
		blocks = append(blocks, table)
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func comparisonSide(value any, kind string) string {
	item, ok := value.(map[string]any)
	if !ok {
		return "Absent"
	}
	var parts []string
	switch kind {
	case "variables", "secrets":
		if item["common"] == true {
			parts = append(parts, "common")
		}
		if envs, ok := stringArray(item["envs"]); ok {
			parts = append(parts, envs...)
		}
	case "instances":
		if alias := text(item["alias"]); alias != "" {
			parts = append(parts, alias)
		}
		parts = append(parts, text(item["directory"]))
	case "items":
		parts = append(parts, text(item["title"]))
		for _, key := range []string{"state", "completion_status"} {
			if item[key] != nil {
				parts = append(parts, plainCell(domainState(item[key])))
			}
		}
	}
	if len(parts) == 0 {
		return "Present"
	}
	return strings.Join(parts, ", ")
}
