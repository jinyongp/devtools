package presentation

import (
	"path/filepath"

	"github.com/jinyongp/devtools/internal/cliui"
)

func formattingReviewBlocks(report map[string]any) []cliui.Block {
	var blocks []cliui.Block
	if path := text(report["config_path"]); path != "" {
		blocks = append(blocks, cliui.Fields(cliui.Field{Label: cliui.Literal("Directory"), Value: cliui.Literal(filepath.Dir(path))}, cliui.Field{Label: cliui.Literal("Input"), Value: cliui.Literal(path)}))
	}
	if changes, ok := report["changes"].([]any); ok && len(changes) > 0 {
		blocks = append(blocks, changeTable(changes))
	} else {
		blocks = append(blocks, cliui.Paragraph(cliui.Literal("No proposed changes.")))
	}
	end := []cliui.Block{cliui.Paragraph(cliui.Literal("No files were changed."))}
	if actions, ok := actionBlocks(report["next_actions"]); ok {
		end = append(end, actions...)
	}
	blocks = append(blocks, cliui.Stack(end...))
	return blocks
}
