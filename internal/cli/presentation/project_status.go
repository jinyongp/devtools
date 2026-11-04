package presentation

import (
	"fmt"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func projectInspectionView(data map[string]any) cliui.Document {
	blocks := []cliui.Block{cliui.Heading(cliui.Literal("Project status"), 1)}
	blocks = append(blocks, selectedFields(data, "profile", "directory"))
	if data["complete"] == false {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Inspection incomplete. Available observations are shown below.")))
	}
	observations, _ := data["observations"].([]any)
	rows := []any{}
	seen := map[string]bool{}
	for _, value := range observations {
		observed, _ := value.(map[string]any)
		owner, _ := observed["owner"].(map[string]any)
		listener, _ := observed["listener"].(map[string]any)
		ownerText := listenerDescription(listener)
		if ownerText == "" && owner["status"] == "verified" {
			ownerText = fmt.Sprintf("%s (PID %v)", text(owner["command"]), owner["pid"])
		}
		if ownerText == "" {
			ownerText = "Unknown"
			if observation, _ := observed["port_observation"].(map[string]any); observation["status"] == "free" {
				ownerText = "None"
			}
		}
		portText := "Unassigned"
		if port := observed["port"]; port != nil {
			observation, _ := observed["port_observation"].(map[string]any)
			status := map[string]string{"in_use": "TCP in use", "free": "no TCP listener", "unavailable": "TCP unverified", "not_checked": "TCP not checked"}[text(observation["status"])]
			portText = fmt.Sprintf("%v (%s)", port, status)
		}
		state := map[string]string{"controlled": "Controllable", "untracked": "Unregistered", "unmanaged": "Unmanaged listener", "unavailable": "Unverified", "available": "Not running"}[text(observed["state"])]
		rows = append(rows, map[string]any{"command": observed["command"], "service": observed["service"], "port": portText, "owner": ownerText, "state": state})
		if id := text(observed["execution_id"]); id != "" {
			seen[id] = true
		}
	}
	if len(rows) > 0 {
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"command", "Command", ""}, {"service", "Service", ""}, {"port", "Port", ""}, {"owner", "Listener / owner", ""}, {"state", "Control", ""}}}, rows))
	}
	for _, value := range observations {
		observed, _ := value.(map[string]any)
		listener, _ := observed["listener"].(map[string]any)
		port, _ := observed["port_observation"].(map[string]any)
		if port["status"] == "in_use" && listener["status"] != "observed" {
			reason := map[string]string{"permission_denied": "Permission to inspect the listener was denied.", "deadline_exceeded": "Listener lookup timed out.", "listener_not_identified": "The OS did not identify the listener.", "observer_unavailable": "Listener lookup is unavailable.", "too_many_listeners": "Only the first 16 listener processes are shown."}[text(listener["reason"])]
			if reason == "" {
				reason = "Listener inspection is incomplete."
			}
			blocks = append(blocks, cliui.Paragraph(cliui.Literal(text(observed["service"])+": "+reason)))
		}
		if observed["owner_profile"] != nil && observed["owner_profile"] != data["profile"] || observed["owner_directory"] != nil && observed["owner_directory"] != data["directory"] {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal(text(observed["service"])+" owner project: "+text(observed["owner_profile"])+" "+text(observed["owner_directory"]))))
		}
		if id := text(observed["execution_id"]); id != "" && observed["state"] != "controlled" {
			blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Execution"), Value: cliui.Literal(id)}))
		}
		if observed["state"] == "controlled" || observed["state"] == "available" {
			continue
		}
		reason := controlReason(text(observed["reason"]))
		if observed["state"] == "unmanaged" {
			reason = "The listener has no verified devtools stop target."
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Literal(text(observed["service"])+": "+reason)))
	}
	var other []any
	items, _ := data["items"].([]any)
	for _, value := range items {
		item, _ := value.(map[string]any)
		if !seen[text(item["id"])] {
			other = append(other, value)
		}
	}
	if len(other) > 0 {
		blocks = append(blocks, cliui.Section(cliui.Literal("Tracked executions"), inventoryTable(inventoryLayout{columns: []ViewColumn{{"command", "Command", ""}, {"mode", "Mode", ""}, {"state", "State", "state"}, {"control.state", "Control", ""}, {"id", "Execution ID", ""}}}, other)))
	}
	if len(rows) == 0 && len(other) == 0 {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("No active executions or assigned services.")))
	}
	diagnostics, _ := data["diagnostics"].([]any)
	for _, value := range diagnostics {
		diagnostic, _ := value.(map[string]any)
		code := text(diagnostic["code"])
		message := "Process inspection unavailable: " + strings.ReplaceAll(code, "_", " ") + "."
		if code == "migration_required" {
			message = "Process records require a storage upgrade. Port observations remain available."
		}
		blocks = append(blocks, cliui.Paragraph(cliui.Literal(message)))
	}
	if actions, ok := actionBlocks(data["next_actions"]); ok {
		blocks = append(blocks, actions...)
	}
	return cliui.NewDocument(blocks...)
}
