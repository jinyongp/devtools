package cli

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/guidance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func guidanceSourceSchema() map[string]any {
	return object(map[string]any{
		"path":     stringSchema(),
		"scope":    stringSchema(),
		"revision": map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
		"bytes":    map[string]any{"type": "integer", "minimum": 0},
		"content":  stringSchema(),
	}, "path", "scope", "revision", "bytes", "content")
}

func guidanceDiagnosticSchema() map[string]any {
	return object(map[string]any{
		"path":    stringSchema(),
		"code":    stringSchema(),
		"message": stringSchema(),
	}, "path", "code", "message")
}

func (a *App) registerGuidance() {
	a.commands = append(a.commands, Command{
		Name:        "guidance resolve",
		Description: "Resolve the ordered AGENTS.md instruction chain for an actual project work target.",
		Options: []Option{
			{Name: "dir", Default: ".", MinLength: 1, Description: "Project root or a directory within a devtools project."},
		},
		Arguments: []Argument{{Name: "target", Required: true}},
		Output: object(map[string]any{
			"target":      stringSchema(),
			"target_dir":  stringSchema(),
			"revision":    map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
			"complete":    map[string]any{"type": "boolean"},
			"sources":     map[string]any{"type": "array", "items": guidanceSourceSchema()},
			"diagnostics": map[string]any{"type": "array", "items": guidanceDiagnosticSchema()},
			"total_bytes": map[string]any{"type": "integer", "minimum": 0},
		}, "target", "target_dir", "revision", "complete", "sources", "diagnostics", "total_bytes"),
		Run: a.resolveGuidance,
	})
}

func (a *App) resolveGuidance(_ context.Context, _ IO, request Request) (any, *protocol.Error) {
	start := request.Options["dir"]
	if start == "" {
		start = "."
	}
	absolute, err := filepath.Abs(start)
	if err != nil {
		return nil, protocol.NewError("invalid_argument", "Cannot resolve project directory.", 2, nil)
	}
	projectContext, projectErr := project.Resolve(absolute, "")
	if projectErr != nil {
		return nil, projectErr
	}
	target := request.Args[0]
	if !filepath.IsAbs(target) {
		target = filepath.Join(absolute, target)
	}
	result, resolveErr := guidance.Resolve(projectContext.Root, target)
	if resolveErr != nil {
		var boundary *guidance.BoundaryError
		if errors.As(resolveErr, &boundary) {
			return nil, protocol.NewError("guidance_boundary", boundary.Message, 3, nil)
		}
		return nil, protocol.NewError("guidance_error", "Cannot resolve AGENTS.md guidance.", 1, nil)
	}
	return result, nil
}
