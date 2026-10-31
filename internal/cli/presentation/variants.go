package presentation

import (
	"fmt"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func variantView(d Definition, data map[string]any) (cliui.Document, bool) {
	switch d.Command {
	case "proxy status", "proxy start", "proxy stop", "proxy restart":
		items, _ := data["items"].([]any)
		single, _ := data["item"].(map[string]any)
		if single != nil {
			items = []any{single}
		}
		if len(items) == 0 {
			return cliui.NewDocument(cliui.Heading(cliui.Literal("Proxy listeners"), 1), cliui.Paragraph(cliui.Literal("No listeners reported."))), true
		}
		rows := []any{}
		for _, value := range items {
			item, _ := value.(map[string]any)
			copy := map[string]any{}
			for key, v := range item {
				copy[key] = v
			}
			if text(copy["name"]) == "" {
				copy["name"] = "default"
			}
			rows = append(rows, copy)
		}
		message := "Proxy listeners"
		if d.Command != "proxy status" {
			message = completedSentence(d)
			if data["changed"] == false {
				message = "No changes to proxy listeners."
			}
		}
		blocks := []cliui.Block{cliui.Heading(cliui.Literal(message), 1), inventoryTable(inventoryLayout{columns: []ViewColumn{{"name", "Listener", ""}, {"state", "State", "state"}, {"port", "HTTP", ""}, {"https_port", "HTTPS", ""}}}, rows)}
		if single != nil {
			blocks = append(blocks, selectedFields(single, "url"))
			if reason := text(single["reason"]); reason != "" {
				blocks = append(blocks, cliui.Paragraph(cliui.Words("Condition: "+reason+".")))
			}
		}
		if data["replayed"] == true {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("Saved result replayed.")))
		}
		blocks = append(blocks, inventoryNotes(data)...)
		return cliui.NewDocument(blocks...), true
	case "proxy up", "proxy down":
		items, _ := data["items"].([]any)
		if items == nil {
			items = []any{data}
		}
		var rows []any
		for _, value := range items {
			result, _ := value.(map[string]any)
			item, _ := result["item"].(map[string]any)
			if item == nil {
				item = result
			}
			ref, _ := item["route_ref"].(map[string]any)
			name := text(item["host"])
			if name == "" {
				name = routeIdentity(ref)
			}
			state := "Unverified"
			if active, ok := item["active"].(bool); ok {
				state = "Inactive"
				if active {
					state = "Active"
				}
			} else if d.Command == "proxy down" && result["changed"] == true {
				state = "Inactive"
			}
			outcome := "Applied"
			if result["changed"] == false {
				outcome = "No change"
			}
			if result["replayed"] == true {
				outcome = "Recorded result"
			}
			rows = append(rows, map[string]any{"route": name, "outcome": outcome, "state": state})
		}
		blocks := []cliui.Block{cliui.Heading(cliui.Literal(completedSentence(d)), 1)}
		if len(rows) > 0 {
			blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"route", "Route", ""}, {"outcome", "Outcome", ""}, {"state", "State", ""}}}, rows))
		}
		blocks = append(blocks, inventoryNotes(data)...)
		return cliui.NewDocument(blocks...), true
	case "proxy clear", "proxy rm":
		items, ok := data["items"].([]any)
		if !ok {
			return cliui.Document{}, false
		}
		message := fmt.Sprintf("Removed %d global routes.", len(items))
		if data["changed"] == false {
			message = "No changes to global routes."
		}
		blocks := []cliui.Block{cliui.Heading(cliui.Literal(message), 1)}
		if len(items) > 0 {
			blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"name", "Route", ""}, {"host", "Host", ""}, {"id", "Route ID", ""}}}, items))
		}
		blocks = append(blocks, inventoryNotes(data)...)
		return cliui.NewDocument(blocks...), true
	case "cleanup preview":
		blocks := []cliui.Block{cliui.Heading(cliui.Literal("Preview: Clean recoverable storage."), 1)}
		blocks = append(blocks, scope(data, map[string]bool{})...)
		items, _ := data["items"].([]any)
		if len(items) == 0 {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("No cleanup candidates.")))
		} else {
			blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"id", "Candidate ID", ""}, {"kind", "Kind", ""}, {"profile", "Profile", ""}, {"source", "Target", ""}, {"bytes", "Bytes", ""}}, optional: []int{4}}, items))
		}
		blocks = append(blocks, selectedFields(data, "id", "expires"), cliui.Paragraph(cliui.Literal("No files were changed.")))
		if retention, ok := data["retention"].(map[string]any); ok && retention["verification"] == "verification_pending" {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("Retention verification is pending; only confirmed cleanup candidates are listed.")))
		}
		blocks = append(blocks, inventoryNotes(data)...)
		return cliui.NewDocument(blocks...), true
	case "cleanup apply":
		items, _ := data["items"].([]any)
		message := fmt.Sprintf("Archived %d cleanup items.", len(items))
		if data["changed"] == false {
			message = "No changes to cleanup state."
		}
		blocks := []cliui.Block{cliui.Heading(cliui.Literal(message), 1)}
		if len(items) > 0 {
			blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"id", "Archive ID", ""}, {"kind", "Kind", ""}, {"source", "Target", ""}}}, items))
		}
		if data["replayed"] == true {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal("Saved result replayed.")))
		}
		blocks = append(blocks, inventoryNotes(data)...)
		return cliui.NewDocument(blocks...), true
	}
	return cliui.Document{}, false
}

func routeIdentity(ref map[string]any) string {
	if ref["scope"] == "global" {
		return "route:" + text(ref["route_id"])
	}
	parts := []string{}
	for _, key := range []string{"profile", "instance_id", "proxy"} {
		if value := text(ref[key]); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "Unavailable"
	}
	return strings.Join(parts, "/")
}
