package cli

import (
	"strings"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/protocol"
)

type rootOptions struct {
	Format          string
	FormatSet       bool
	IsolatedRoot    string
	CommandArgs     []string
	OriginalOffsets []int
	formatIndex     int
}

func formatError(message string, offset int, environment bool) *protocol.Error {
	source := map[string]any{"kind": "argv", "path": []string{"format"}, "offset": offset}
	if environment {
		source = map[string]any{"kind": "system", "path": []string{"env", "DEVTOOLS_FORMAT"}}
	}
	return protocol.NewError("invalid_argument", message, 2, map[string]any{
		"field": "format", "phase": "parse", "reason": "invalid_value",
		"effects": "none", "retry": "after_input_change", "sources": []any{source},
	})
}

func parseRootOptions(args []string) (rootOptions, *protocol.Error) {
	root := rootOptions{}
	i := 0
	for i < len(args) {
		name, value, inline := strings.Cut(args[i], "=")
		if name != "--format" && name != "--isolated-root" {
			break
		}
		index := i
		i++
		if name == "--format" && root.FormatSet {
			return root, formatError("Specify --format once.", index, false)
		}
		if name == "--isolated-root" && root.IsolatedRoot != "" {
			return root, argumentError("Specify the isolation root once.", "isolated-root")
		}
		if !inline {
			if i >= len(args) || strings.HasPrefix(args[i], "--") {
				if name == "--format" {
					return root, formatError("Option --format requires auto, text, or json.", index, false)
				}
				return root, argumentError("Supply an isolation root and command.", "isolated-root")
			}
			value = args[i]
			i++
		}
		if name == "--format" {
			if _, err := presentation.Select(presentation.FormatInput{Present: true, Raw: value}, presentation.FormatInput{}); err != nil {
				return root, formatError("Option --format requires auto, text, or json.", index, false)
			}
			root.Format, root.FormatSet = value, true
			root.formatIndex = index
		} else {
			if value == "" {
				return root, argumentError("Supply an isolation root and command.", "isolated-root")
			}
			root.IsolatedRoot = value
		}
	}
	if root.IsolatedRoot != "" && i == len(args) {
		return root, argumentError("Supply an isolation root and command.", "isolated-root")
	}
	root.CommandArgs = append([]string(nil), args[i:]...)
	root.OriginalOffsets = make([]int, len(root.CommandArgs)+1)
	for j := range root.OriginalOffsets {
		root.OriginalOffsets[j] = i + j
	}
	return root, nil
}

func formatOffset(root rootOptions) int { return root.formatIndex }

// Only parser-relative sources enter here. Handler Invocation sources already
// point to original argv and must never pass through a final common remapper.
func mapArgumentSources(err *protocol.Error, offsets []int) {
	if err == nil {
		return
	}
	if sources, ok := err.Details["sources"].([]any); ok {
		for _, value := range sources {
			source, ok := value.(map[string]any)
			if !ok || source["kind"] != "argv" {
				continue
			}
			for _, field := range []string{"offset", "end_offset"} {
				if index, ok := source[field].(int); ok && index >= 0 && index < len(offsets) {
					source[field] = offsets[index]
				}
			}
		}
	}
}
