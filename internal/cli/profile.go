package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type profileTransferItem struct {
	Profile       string `json:"profile"`
	SourceProfile string `json:"source_profile,omitempty"`
	Path          string `json:"path,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	Values        bool   `json:"values"`
	Tasks         bool   `json:"tasks"`
}

func profileTransferFields() map[string]any {
	return map[string]any{
		"profile": stringSchema(),
		"values":  map[string]any{"type": "boolean"},
		"tasks":   map[string]any{"type": "boolean"},
	}
}

func profileExportItemSchema() map[string]any {
	fields := profileTransferFields()
	fields["path"] = stringSchema()
	fields["created_at"] = stringSchema()
	return object(fields, "profile", "values", "tasks", "path", "created_at")
}

func profileImportItemSchema() map[string]any {
	fields := profileTransferFields()
	fields["source_profile"] = stringSchema()
	return object(fields, "profile", "values", "tasks", "source_profile")
}

func (a *App) registerProfiles() {
	a.commands = append(a.commands,
		Command{
			Name:        "profile export",
			Description: "Encrypt one profile for transfer to another environment.",
			Options: []Option{
				profileIdentifierOption("profile", false),
				{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory used when profile is omitted."},
				filePathOption("output", false),
				filePathOption("recipient-file", false),
			},
			Output: changedItemOutput(profileExportItemSchema()),
			Run:    a.exportProfile,
		},
		Command{
			Name:        "profile import",
			Description: "Import one encrypted profile, keeping its name unless --as is supplied.",
			Options: []Option{
				filePathOption("file", true),
				filePathOption("identity-file", true),
				profileIdentifierOption("profile", false),
				profileIdentifierOption("as", false),
				{Name: "replace", Boolean: true, Description: "Replace an existing target profile after creating a safety backup."},
				{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe import."},
			},
			Output: object(map[string]any{
				"item":          profileImportItemSchema(),
				"changed":       map[string]any{"type": "boolean"},
				"replayed":      map[string]any{"type": "boolean"},
				"safety_backup": stringSchema(),
			}, "item", "changed", "replayed", "safety_backup"),
			Run: a.importProfile,
		},
	)
}

func (a *App) exportProfile(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	profile := request.Options["profile"]
	if profile == "" {
		p, err := project.Resolve(request.Options["dir"], "")
		if err != nil {
			return nil, err
		}
		profile = p.Profile
	}
	engine, err := a.backupEngine()
	if err != nil {
		return nil, err
	}
	result, failure := engine.Create(ctx, profile, request.Options["output"], request.Options["recipient-file"])
	if failure != nil {
		return nil, failure
	}
	if len(result.Profiles) != 1 || result.Profiles[0].Profile != profile {
		return nil, protocol.NewError("internal_error", "Profile export did not produce exactly one profile.", 1, nil)
	}
	item := profileTransferItem{Profile: profile, Path: result.Path, CreatedAt: result.CreatedAt, Values: result.Profiles[0].Values, Tasks: result.Profiles[0].Tasks}
	return map[string]any{"item": item, "changed": true}, nil
}

func (a *App) importProfile(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	engine, engineErr := a.backupEngine()
	if engineErr != nil {
		return nil, engineErr
	}
	result, importErr := engine.Import(ctx, backup.ImportRequest{
		Path:         request.Options["file"],
		IdentityPath: request.Options["identity-file"],
		Source:       request.Options["profile"],
		Target:       request.Options["as"],
		RequestID:    request.Options["request-id"],
		Replace:      request.Options["replace"] == "true",
	})
	if importErr != nil {
		return nil, importErr
	}
	item := profileTransferItem{Profile: result.Target, SourceProfile: result.Source.Profile, Values: result.Source.Values, Tasks: result.Source.Tasks}
	return map[string]any{"item": item, "changed": result.Plan.Applied, "replayed": result.Plan.Replayed, "safety_backup": result.Plan.SafetyBackup}, nil
}
