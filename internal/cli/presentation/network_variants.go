package presentation

import "github.com/jinyongp/devtools/internal/cliui"

func hostsWriteMessage(effect any) string {
	switch effect {
	case "performed":
		return "The OS hosts file was updated."
	case "not_performed":
		return "No OS hosts write was needed."
	default:
		return "The hosts step is recorded. The OS write could not be attributed to this invocation."
	}
}

func hostsStatusView(data, item map[string]any) cliui.Document {
	if item["state"] == "none" {
		return cliui.NewDocument(outcomeHeading("No pending hosts operation."))
	}
	heading := "A hosts operation is pending."
	if item["pending"] == nil {
		heading = "The pending hosts operation could not be inspected."
	}
	blocks := []cliui.Block{outcomeHeading(heading)}
	if ref, ok := item["route_ref"].(map[string]any); ok {
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Route"), Value: cliui.Literal(routeIdentity(ref))}))
	}
	blocks = append(blocks, selectedFields(item, "state", "origin_request_id", "recovery_request_id"))
	if rows, ok := item["changes"].([]any); ok && len(rows) > 0 {
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{
			{"action", "Change", ""}, {"host", "Host", ""}, {"address", "Address", ""}, {"owner", "Owner", ""},
		}}, rows))
	}
	if item["recovery_action"] == "blocked" {
		reason := map[string]string{
			"references_unavailable": "Restore access to the private hosts reference journal and check status again.",
			"hosts_unavailable":      "Check that the OS hosts file is readable and its writer has finished.",
			"ownership_unknown":      "Identify the owner of the managed hosts block before changing it.",
			"mapping_conflict":       "Compare the listed mappings with the conflicting entries in the OS hosts file.",
			"divergence_ambiguous":   "Compare the managed block with both the saved and planned mappings before changing it.",
			"isolated_write":         "This operation requires an OS hosts write, which is unavailable in this isolated store.",
			"helper_input_too_large": "The saved change exceeds the size supported by the OS hosts helper.",
		}[text(item["reason"])]
		if reason != "" {
			blocks = append(blocks, cliui.Paragraph(cliui.Literal(reason)))
		}
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func hostsPreviewView(data map[string]any) cliui.Document {
	blocks := []cliui.Block{outcomeHeading("Preview: Update hosts mappings.")}
	if mappings, ok := data["mappings"].([]any); ok && len(mappings) > 0 {
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"host", "Host", ""}, {"address", "Address", ""}}}, mappings))
	} else {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("No hosts mappings required.")))
	}
	if data["changed"] == false {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("The OS hosts file would stay the same.")))
	}
	blocks = append(blocks, selectedFields(data, "digest"), cliui.Paragraph(cliui.Literal("No files were changed.")))
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}

func setupCheckView(item map[string]any) cliui.Document {
	boolLabel := func(value any) string {
		if flag, ok := value.(bool); ok {
			if flag {
				return "Enabled"
			}
			return "Disabled"
		}
		return "Unavailable"
	}
	mode := "Unrecognized: " + text(item["mode"])
	capability := boolLabel(item["enabled"])
	switch item["mode"] {
	case "native":
		mode, capability = "Native", "Not applicable"
	case "capability":
		mode = "Capability"
	}
	bind := "Unrecognized: " + text(item["bind_check"])
	if item["bind_check"] == "performed_when_listener_starts" {
		bind = "Checked when the listener starts"
	}
	blocks := []cliui.Block{cliui.Heading(cliui.Literal("Proxy setup check"), 1), cliui.Fields(
		cliui.Field{Label: cliui.Literal("Setup mode"), Value: cliui.Literal(mode)},
		cliui.Field{Label: cliui.Literal("Intent"), Value: cliui.Literal(boolLabel(item["intent_enabled"]))},
		cliui.Field{Label: cliui.Literal("Capability"), Value: cliui.Literal(capability)},
		cliui.Field{Label: cliui.Literal("Bind check"), Value: cliui.Literal(bind)},
	)}
	if item["recovery_required"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Words("Setup recovery is required before its intent can be satisfied.")))
	}
	if item["executable_changed"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Words("The executable changed since setup; its current capability must be reviewed.")))
	}
	return cliui.NewDocument(blocks...)
}

func teardownView(data, plan map[string]any) cliui.Document {
	preview := data["dry_run"] == true
	message, action := "Removed selected proxy resources.", "Removed"
	if preview {
		message, action = "Preview: Remove proxy setup.", "Remove"
	}
	if !preview && data["changed"] == false {
		message, action = "No changes to proxy setup.", "No change"
	}
	if data["replayed"] == true {
		message, action = "Recorded proxy teardown result.", "Recorded removal"
	}
	blocks := []cliui.Block{outcomeHeading(message)}
	selection, _ := plan["selection"].(map[string]any)
	var rows []any
	var conditions []cliui.Block
	for _, key := range []string{"listeners", "routes", "exposures"} {
		items, _ := plan[key].([]any)
		for _, value := range items {
			item, _ := value.(map[string]any)
			listener, resource := text(item["listener"]), ""
			switch key {
			case "listeners":
				listener, resource = text(item["name"]), "Listener"
			case "routes":
				resource = "Route " + routeIdentity(item)
			case "exposures":
				resource = "Exposure " + text(item["id"])
			}
			if listener == "" && selection["all"] != true {
				listener = text(selection["listener"])
				if listener == "" {
					listener = "default"
				}
			}
			if listener == "" {
				listener = "Unavailable"
			}
			rows = append(rows, map[string]any{"listener": listener, "resource": resource, "action": action})
		}
	}
	if fingerprint := text(plan["ca_fingerprint"]); fingerprint != "" {
		if selection["remove_ca"] == true {
			rows = append(rows, map[string]any{"listener": "Shared", "resource": "CA " + fingerprint, "action": action})
			conditions = append(conditions, cliui.Paragraph(cliui.Words("Removing the shared CA also requires OS untrust.")))
		} else {
			conditions = append(conditions, cliui.Paragraph(cliui.Words("Shared CA removal is not selected.")))
		}
	}
	if len(rows) > 0 {
		blocks = append(blocks, inventoryTable(inventoryLayout{columns: []ViewColumn{{"listener", "Listener", ""}, {"resource", "Resource", ""}, {"action", "Action", ""}}}, rows))
	} else {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("No resources selected.")))
	}
	blocks = append(blocks, conditions...)
	if preview {
		blocks = append(blocks, selectedFields(plan, "digest"), cliui.Paragraph(cliui.Literal("No routes were changed.")))
	}
	if data["replayed"] == true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Saved result replayed.")))
	}
	blocks = append(blocks, inventoryNotes(data)...)
	return cliui.NewDocument(blocks...)
}
