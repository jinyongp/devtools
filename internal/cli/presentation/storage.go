package presentation

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func storageReportBlocks(data map[string]any) []cliui.Block {
	state := "Storage could not be verified."
	if data["ready"] == true {
		state = "Storage is current."
	} else if data["state"] == "migration_required" {
		state = "Storage upgrade required."
	} else if data["changed"] == true || data["effects"] == "committed" || data["effects"] == "unknown" {
		state = "Storage upgrade is incomplete."
	}
	blocks := []cliui.Block{outcomeHeading(state), selectedFields(data, "directory", "effects")}
	items, _ := data["items"].([]any)
	if data["ready"] != true {
		priority := func(item any) int {
			row, _ := item.(map[string]any)
			switch row["state"] {
			case "current":
				return 2
			case "migration_required":
				return 1
			default:
				return 0
			}
		}
		items = slices.Clone(items)
		sort.SliceStable(items, func(i, j int) bool { return priority(items[i]) < priority(items[j]) })
	}
	current := 0
	for _, item := range items {
		row, _ := item.(map[string]any)
		if data["ready"] != true && row["state"] == "current" && text(row["reason"]) == "" && row["diagnostic"] == nil {
			current++
			continue
		}
		label := text(row["domain"])
		if profile := text(row["profile"]); profile != "" {
			label += " / " + profile
		}
		if resource := text(row["resource"]); resource != "" {
			label += " / " + resource
		}
		value := text(row["source"]) + " → " + text(row["target"]) + ": " + strings.ReplaceAll(text(row["state"]), "_", " ")
		if reason := text(row["reason"]); reason != "" {
			value += " (" + strings.ReplaceAll(reason, "_", " ") + ")"
		}
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal(label), Value: cliui.Literal(value)}))
		blocks = append(blocks, observationDiagnosticBlocks(row["diagnostic"])...)
	}
	if current == 1 {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("1 storage item is current.")))
	} else if current > 1 {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal(fmt.Sprintf("%d storage items are current.", current))))
	}
	if data["state"] == "migration_required" {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Run devtools doctor --storage --fix in the same storage context to apply or resume the upgrade.")))
	}
	return blocks
}

func observationDiagnosticBlocks(value any) []cliui.Block {
	diagnostic, _ := value.(map[string]any)
	details, _ := diagnostic["details"].(map[string]any)
	if len(details) == 0 {
		return nil
	}
	fields := []cliui.Field{}
	for _, entry := range [][2]string{{"reason", "Cause"}, {"state", "Storage state"}, {"domain", "Storage"}, {"resource", "Resource"}, {"effects", "Effects"}, {"retry", "Retry"}} {
		if value := text(details[entry[0]]); value != "" {
			fields = append(fields, cliui.Field{Label: cliui.Literal(entry[1]), Value: cliui.Literal(strings.ReplaceAll(value, "_", " "))})
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return []cliui.Block{cliui.Fields(fields...)}
}
