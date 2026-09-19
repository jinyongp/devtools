package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/skills"
)

const skillNamePattern = "^[a-z0-9]+(?:-[a-z0-9]+)*$"

func skillResourceSchema() map[string]any {
	return object(map[string]any{
		"path":       stringSchema(),
		"size":       map[string]any{"type": "integer", "minimum": 0},
		"sha256":     map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
		"executable": map[string]any{"type": "boolean"},
	}, "path", "size", "sha256", "executable")
}

func skillSummarySchema(details bool) map[string]any {
	properties := map[string]any{
		"name":           map[string]any{"type": "string", "pattern": skillNamePattern},
		"description":    map[string]any{"type": "string"},
		"scope":          map[string]any{"enum": []string{"project", "user"}},
		"root":           stringSchema(),
		"revision":       map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
		"license":        stringSchema(),
		"compatibility":  stringSchema(),
		"metadata":       map[string]any{"type": "object", "additionalProperties": stringSchema()},
		"allowed_tools":  stringSchema(),
		"resource_count": map[string]any{"type": "integer", "minimum": 0},
		"total_bytes":    map[string]any{"type": "integer", "minimum": 0},
	}
	if details {
		properties["content"] = stringSchema()
		properties["resources"] = map[string]any{"type": "array", "items": skillResourceSchema()}
	}
	return object(properties, "name", "description", "scope", "root", "revision", "resource_count", "total_bytes")
}

func skillDiagnosticSchema() map[string]any {
	return object(map[string]any{
		"scope":   map[string]any{"enum": []string{"project", "user"}},
		"path":    stringSchema(),
		"code":    stringSchema(),
		"message": stringSchema(),
	}, "scope", "path", "code", "message")
}

func skillShadowSchema() map[string]any {
	return object(map[string]any{
		"name":           stringSchema(),
		"selected_scope": map[string]any{"enum": []string{"project", "user"}},
		"selected_root":  stringSchema(),
		"shadowed_scope": map[string]any{"enum": []string{"project", "user"}},
		"shadowed_root":  stringSchema(),
	}, "name", "selected_scope", "selected_root", "shadowed_scope", "shadowed_root")
}

func (a *App) registerSkills() {
	projectOption := Option{Name: "dir", Default: ".", MinLength: 1, Description: "Project root or a directory within a devtools project."}
	a.commands = append(a.commands,
		Command{
			Name:        "skill list",
			Description: "List effective Agent Skills from project and user standard locations.",
			Options:     []Option{projectOption},
			Output: object(map[string]any{
				"items":       map[string]any{"type": "array", "items": skillSummarySchema(false)},
				"diagnostics": map[string]any{"type": "array", "items": skillDiagnosticSchema()},
				"shadowed":    map[string]any{"type": "array", "items": skillShadowSchema()},
			}, "items", "diagnostics", "shadowed"),
			Run: a.listSkills,
		},
		Command{
			Name:        "skill inspect",
			Description: "Read one effective Agent Skill and its bounded resource inventory.",
			Options:     []Option{projectOption},
			Arguments:   []Argument{{Name: "skill-name", Required: true, Pattern: skillNamePattern}},
			Output: object(map[string]any{
				"item":        skillSummarySchema(true),
				"diagnostics": map[string]any{"type": "array", "items": skillDiagnosticSchema()},
				"shadowed":    map[string]any{"type": "array", "items": skillShadowSchema()},
			}, "item", "diagnostics", "shadowed"),
			Run: a.inspectSkill,
		},
		Command{
			Name:        "skill register",
			Description: "Register a validated local Agent Skill into project or user standard scope without overwriting.",
			Options: []Option{
				projectOption,
				{Name: "scope", Required: true, Pattern: "^(project|user)$", Description: "Registration scope: project or user."},
			},
			Arguments: []Argument{{Name: "source", Required: true}},
			Output:    changedItemOutput(skillSummarySchema(true)),
			Run:       a.registerSkill,
		},
	)
}

func skillProjectRoot(directory string) (string, *protocol.Error) {
	if directory == "" {
		directory = "."
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", protocol.NewError("invalid_argument", "Cannot resolve project directory.", 2, nil)
	}
	if resolved, projectErr := project.Resolve(absolute, ""); projectErr == nil {
		return resolved.Root, nil
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", protocol.NewError("invalid_argument", "Cannot resolve project directory.", 2, nil)
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", protocol.NewError("invalid_argument", "Project directory must exist.", 2, nil)
	}
	return canonical, nil
}

func skillHome() (string, *protocol.Error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", protocol.NewError("io_error", "Cannot resolve the user home directory.", 1, nil)
	}
	canonical, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", protocol.NewError("io_error", "Cannot resolve the user home directory.", 1, nil)
	}
	return canonical, nil
}

func skillProtocolError(err error) *protocol.Error {
	var validation *skills.ValidationError
	if errors.As(err, &validation) {
		return protocol.NewError("skill_invalid", validation.Message, 3, map[string]any{"skill_code": validation.Code, "path": validation.Path})
	}
	return protocol.NewError("skill_error", "Cannot access Agent Skills.", 1, nil)
}

func skillCatalog(directory string) (skills.Catalog, *protocol.Error) {
	root, err := skillProjectRoot(directory)
	if err != nil {
		return skills.Catalog{}, err
	}
	home, err := skillHome()
	if err != nil {
		return skills.Catalog{}, err
	}
	catalog, discoverErr := skills.Discover(root, home)
	if discoverErr != nil {
		return skills.Catalog{}, skillProtocolError(discoverErr)
	}
	return catalog, nil
}

func (a *App) listSkills(_ context.Context, _ IO, request Request) (any, *protocol.Error) {
	catalog, err := skillCatalog(request.Options["dir"])
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

func (a *App) inspectSkill(_ context.Context, _ IO, request Request) (any, *protocol.Error) {
	catalog, err := skillCatalog(request.Options["dir"])
	if err != nil {
		return nil, err
	}
	name := request.Args[0]
	for _, item := range catalog.Items {
		if item.Name != name {
			continue
		}
		details, inspectErr := skills.Inspect(item.Root, item.Scope)
		if inspectErr != nil {
			return nil, skillProtocolError(inspectErr)
		}
		return map[string]any{"item": details, "diagnostics": catalog.Diagnostics, "shadowed": catalog.Shadowed}, nil
	}
	return nil, protocol.NewError("skill_not_found", "The selected Agent Skill is not registered.", 3, nil)
}

func (a *App) registerSkill(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	root, err := skillProjectRoot(request.Options["dir"])
	if err != nil {
		return nil, err
	}
	home, err := skillHome()
	if err != nil {
		return nil, err
	}
	scope := skills.Scope(request.Options["scope"])
	item, registerErr := skills.Register(ctx, request.Args[0], root, home, scope)
	if registerErr != nil {
		if errors.Is(registerErr, skills.ErrExists) {
			return nil, protocol.NewError("skill_exists", "The Agent Skill already exists in the selected scope.", 3, nil)
		}
		return nil, skillProtocolError(registerErr)
	}
	return map[string]any{"item": item, "changed": true}, nil
}
