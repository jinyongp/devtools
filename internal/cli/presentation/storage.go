package presentation

import (
	"strings"

	"github.com/jinyongp/devtools/internal/cliui"
)

func storageReportBlocks(data map[string]any) []cliui.Block {
	state := "Storage upgrade required."
	if data["ready"] == true {
		state = "Storage is current."
	}
	blocks := []cliui.Block{outcomeHeading(state), selectedFields(data, "directory", "effects")}
	items, _ := data["items"].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		label := text(row["domain"])
		if profile := text(row["profile"]); profile != "" {
			label += " / " + profile
		}
		value := text(row["source"]) + " → " + text(row["target"]) + ": " + strings.ReplaceAll(text(row["state"]), "_", " ")
		if reason := text(row["reason"]); reason != "" {
			value += " (" + strings.ReplaceAll(reason, "_", " ") + ")"
		}
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal(label), Value: cliui.Literal(value)}))
	}
	if data["ready"] != true {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("Run devtools doctor --storage --fix in the same storage context to apply or resume the upgrade.")))
	}
	return blocks
}
