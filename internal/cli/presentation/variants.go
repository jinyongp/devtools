package presentation

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func variantView(d Definition, data map[string]any) (cliui.Document, bool) {
	switch d.Command {
	case "proxy hosts preview":
		return hostsPreviewView(data), true
	case "proxy hosts apply", "proxy hosts remove":
		if ref, ok := data["route_ref"].(map[string]any); ok {
			message := "Applied hosts mappings."
			if d.Command == "proxy hosts remove" {
				message = "Removed hosts mappings."
			}
			if data["changed"] == false {
				message = "No changes to hosts mappings."
			}
			blocks := []cliui.Block{outcomeHeading(message), cliui.Fields(cliui.Field{Label: cliui.Literal("Route"), Value: cliui.Literal(routeIdentity(ref))})}
			if data["replayed"] == true {
				blocks = append(blocks, cliui.Paragraph(cliui.Literal("Saved result replayed.")))
			}
			blocks = append(blocks, inventoryNotes(data)...)
			return cliui.NewDocument(blocks...), true
		}
	case "proxy setup":
		if item, ok := data["item"].(map[string]any); ok && item["bind_check"] != nil {
			return setupCheckView(item), true
		}
	case "proxy teardown":
		if plan, ok := data["item"].(map[string]any); ok {
			return teardownView(data, plan), true
		}
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
			copy["http_url"] = item["url"]
			if port, err := strconv.Atoi(fmt.Sprint(item["https_port"])); err == nil {
				if port == 0 {
					copy["https_url"] = "Disabled"
				} else if address, err := url.Parse(text(item["url"])); err == nil && address.Hostname() != "" {
					address.Scheme, address.Host = "https", net.JoinHostPort(address.Hostname(), strconv.Itoa(port))
					copy["https_url"] = address.String()
				}
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
		blocks := []cliui.Block{outcomeHeading(message), inventoryTable(inventoryLayout{columns: []ViewColumn{{"name", "Listener", ""}, {"state", "State", "state"}, {"http_url", "HTTP", ""}, {"https_url", "HTTPS", ""}}}, rows)}
		if single != nil {
			if reason := text(single["reason"]); reason != "" {
				blocks = append(blocks, cliui.Paragraph(cliui.Words("Condition: "+reason+".")))
			}
		}
		if len(rows) > 1 {
			blocks = append(blocks, cliui.Paragraph(cliui.Styled(fmt.Sprintf("%d listeners", len(rows)), cliui.Muted)))
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
			if text(ref["scope"]) != "" {
				if name != "" {
					name = routeIdentity(ref) + " (" + name + ")"
				} else {
					name = routeIdentity(ref)
				}
			} else if name == "" {
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
		blocks := []cliui.Block{outcomeHeading(message)}
		if len(items) > 0 {
			rows := make([]any, 0, len(items))
			for _, value := range items {
				item, _ := value.(map[string]any)
				name := "Global " + text(item["name"])
				if id := text(item["id"]); id != "" {
					name += " (" + id + ")"
				}
				outcome := "Removed"
				if data["changed"] == false {
					outcome = "No change"
				}
				if data["replayed"] == true {
					outcome = "Recorded removal"
				}
				rows = append(rows, map[string]any{"route": name, "outcome": outcome})
			}
			blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"route", "Route", ""}, {"outcome", "Outcome", ""}}}, rows))
		}
		blocks = append(blocks, inventoryNotes(data)...)
		return cliui.NewDocument(blocks...), true
	case "cleanup preview":
		blocks := []cliui.Block{outcomeHeading("Preview: Clean recoverable storage.")}
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
		blocks := []cliui.Block{outcomeHeading(message)}
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
