package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/backup"
	profilecatalog "github.com/jinyongp/devtools/internal/profiles"
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

func profileSummarySchema() map[string]any {
	return object(map[string]any{
		"profile":              stringSchema(),
		"values":               map[string]any{"type": "boolean"},
		"tasks":                map[string]any{"type": "boolean"},
		"env_count":            map[string]any{"type": "integer", "minimum": 0},
		"instance_count":       map[string]any{"type": "integer", "minimum": 0},
		"process_count":        map[string]any{"type": "integer", "minimum": 0},
		"active_process_count": map[string]any{"type": "integer", "minimum": 0},
	}, "profile", "values", "tasks", "env_count", "instance_count", "process_count", "active_process_count")
}

func profileDiffSchema() map[string]any {
	valueScope := object(map[string]any{
		"key":    stringSchema(),
		"common": map[string]any{"type": "boolean"},
		"envs":   map[string]any{"type": "array", "items": stringSchema()},
	}, "key", "common", "envs")
	taskMetadata := object(map[string]any{
		"id":                stringSchema(),
		"kind":              stringSchema(),
		"title":             stringSchema(),
		"state":             stringSchema(),
		"completion_status": stringSchema(),
	}, "id", "kind", "title", "state", "completion_status")
	instance := object(map[string]any{
		"alias":     map[string]any{"type": []string{"string", "null"}},
		"directory": stringSchema(),
	}, "alias", "directory")
	nameChange := object(map[string]any{"name": stringSchema(), "action": map[string]any{"enum": []string{"added", "removed"}}}, "name", "action")
	valueChange := object(map[string]any{"key": stringSchema(), "action": map[string]any{"enum": []string{"added", "removed", "changed"}}, "left": valueScope, "right": valueScope}, "key", "action")
	taskChange := object(map[string]any{"id": stringSchema(), "kind": stringSchema(), "action": map[string]any{"enum": []string{"added", "removed", "changed"}}, "left": taskMetadata, "right": taskMetadata}, "id", "kind", "action")
	instanceChange := object(map[string]any{"action": map[string]any{"enum": []string{"added", "removed"}}, "left": instance, "right": instance}, "action")
	return object(map[string]any{
		"left":      stringSchema(),
		"right":     stringSchema(),
		"different": map[string]any{"type": "boolean"},
		"envs":      map[string]any{"type": "array", "items": nameChange},
		"variables": map[string]any{"type": "array", "items": valueChange},
		"secrets":   map[string]any{"type": "array", "items": valueChange},
		"items":     map[string]any{"type": "array", "items": taskChange},
		"instances": map[string]any{"type": "array", "items": instanceChange},
	}, "left", "right", "different", "envs", "variables", "secrets", "items", "instances")
}

func profileDetailSchema() map[string]any {
	valueScope := object(map[string]any{
		"key":    stringSchema(),
		"common": map[string]any{"type": "boolean"},
		"envs":   map[string]any{"type": "array", "items": stringSchema()},
	}, "key", "common", "envs")
	taskCount := object(map[string]any{
		"kind":  stringSchema(),
		"state": stringSchema(),
		"count": map[string]any{"type": "integer", "minimum": 0},
	}, "kind", "state", "count")
	instance := object(map[string]any{
		"instance_id": stringSchema(),
		"alias":       map[string]any{"type": []string{"string", "null"}},
		"directory":   stringSchema(),
	}, "instance_id", "alias", "directory")
	readiness := object(map[string]any{
		"ready":      map[string]any{"type": "boolean"},
		"checked_at": stringSchema(),
		"reason":     stringSchema(),
		"exit_code":  map[string]any{"type": []string{"integer", "null"}},
	}, "ready", "checked_at", "reason", "exit_code")
	process := object(map[string]any{
		"execution_id":     stringSchema(),
		"command":          stringSchema(),
		"env":              stringSchema(),
		"state":            stringSchema(),
		"ready_configured": map[string]any{"type": "boolean"},
		"readiness":        readiness,
	}, "execution_id", "command", "env", "state", "ready_configured")
	fields := profileSummarySchema()["properties"].(map[string]any)
	fields["envs"] = map[string]any{"type": "array", "items": stringSchema()}
	fields["variables"] = map[string]any{"type": "array", "items": valueScope}
	fields["secrets"] = map[string]any{"type": "array", "items": valueScope}
	fields["task_counts"] = map[string]any{"type": "array", "items": taskCount}
	fields["instances"] = map[string]any{"type": "array", "items": instance}
	fields["processes"] = map[string]any{"type": "array", "items": process}
	return object(fields, "profile", "values", "tasks", "env_count", "instance_count", "process_count", "active_process_count", "envs", "variables", "secrets", "task_counts", "instances", "processes")
}

func (a *App) profileCatalog() (profilecatalog.Catalog, *protocol.Error) {
	directory, err := a.dataDirectory()
	if err != nil {
		return profilecatalog.Catalog{}, err
	}
	return profilecatalog.Catalog{Data: filepath.Dir(directory)}, nil
}

func (a *App) registerProfiles() {
	a.commands = append(a.commands,
		Command{
			Name:        "profile list",
			Description: "List known profiles without returning stored values.",
			Output:      object(map[string]any{"items": map[string]any{"type": "array", "items": profileSummarySchema()}}, "items"),
			Run:         a.listProfiles,
		},
		Command{
			Name:        "profile inspect",
			Description: "Inspect one profile's metadata, instances, and managed processes without returning values.",
			Arguments:   []Argument{{Name: "profile", Required: true, Pattern: project.ProfilePattern}},
			Output:      itemOutput(profileDetailSchema()),
			Run:         a.inspectProfile,
		},
		Command{
			Name:        "profile diff",
			Description: "Compare two profiles without returning stored values or runtime process state.",
			Arguments: []Argument{
				{Name: "left", Required: true, Pattern: project.ProfilePattern},
				{Name: "right", Required: true, Pattern: project.ProfilePattern},
			},
			Output: profileDiffSchema(),
			Run:    a.diffProfiles,
		},
		Command{
			Name:        "profile export",
			Description: "Encrypt one profile for transfer to another environment.",
			Options: []Option{
				profileIdentifierOption("profile", false),
				{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory used when profile is omitted."},
				filePathOption("output", false),
				filePathOption("recipient-file", false),
				publicRecipientOption(),
			},
			InputOneOf: recipientInputOneOf(),
			Output:     changedItemOutput(profileExportItemSchema()),
			Run:        a.exportProfile,
		},
		Command{
			Name:        "profile import",
			Description: "Preview an encrypted profile import; apply requires the preview digest and request ID.",
			Options: []Option{
				filePathOption("file", true),
				filePathOption("identity-file", true),
				profileIdentifierOption("profile", false),
				profileIdentifierOption("as", false),
				{Name: "replace", Boolean: true, Description: "Permit replacement of an existing target profile after creating a safety backup."},
				{Name: "apply", MinLength: 64, MaxLength: 64, Pattern: `^[0-9a-f]+$`, Description: "Apply the exact digest returned by preview."},
				{Name: "request-id", Pattern: uuidPattern, Description: "UUID required when applying; reuse it for retries."},
			},
			InputOneOf: []map[string]any{
				{"not": map[string]any{"anyOf": []map[string]any{{"required": []string{"apply"}}, {"required": []string{"request-id"}}}}},
				{"required": []string{"apply", "request-id"}},
			},
			Output: object(map[string]any{
				"item":          profileImportItemSchema(),
				"digest":        stringSchema(),
				"target_exists": map[string]any{"type": "boolean"},
				"diff":          profileDiffSchema(),
				"changed":       map[string]any{"type": "boolean"},
				"replayed":      map[string]any{"type": "boolean"},
				"safety_backup": stringSchema(),
			}, "item", "digest", "target_exists", "diff", "changed", "replayed", "safety_backup"),
			Run: a.importProfile,
		},
	)
}

func (a *App) listProfiles(_ context.Context, _ IO, _ Request) (any, *protocol.Error) {
	catalog, err := a.profileCatalog()
	if err != nil {
		return nil, err
	}
	items, listErr := catalog.List()
	if listErr != nil {
		return nil, listErr
	}
	return map[string]any{"items": items}, nil
}

func (a *App) inspectProfile(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	catalog, err := a.profileCatalog()
	if err != nil {
		return nil, err
	}
	item, inspectErr := catalog.Inspect(ctx, request.Args[0])
	if inspectErr != nil {
		return nil, inspectErr
	}
	return map[string]any{"item": item}, nil
}

func (a *App) diffProfiles(_ context.Context, _ IO, request Request) (any, *protocol.Error) {
	catalog, err := a.profileCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.Diff(request.Args[0], request.Args[1])
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
	result, failure := engine.CreateWithRecipient(ctx, backup.CreateOptions{Profile: profile, Output: request.Options["output"], RecipientPath: request.Options["recipient-file"], Recipient: request.Options["recipient"]})
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
	if (request.Options["apply"] != "") != (request.Options["request-id"] != "") {
		return nil, argumentError("Apply and request-id must be supplied together.", "")
	}
	engine, engineErr := a.backupEngine()
	if engineErr != nil {
		return nil, engineErr
	}
	result, importErr := engine.Import(ctx, backup.ImportRequest{
		Path:         request.Options["file"],
		IdentityPath: request.Options["identity-file"],
		Source:       request.Options["profile"],
		Target:       request.Options["as"],
		Expected:     request.Options["apply"],
		RequestID:    request.Options["request-id"],
		Replace:      request.Options["replace"] == "true",
	})
	if importErr != nil {
		return nil, importErr
	}
	if len(result.Plan.Targets) != 1 {
		return nil, protocol.NewError("internal_error", "Profile import did not resolve exactly one target.", 1, nil)
	}
	item := profileTransferItem{Profile: result.Target, SourceProfile: result.Source.Profile, Values: result.Source.Values, Tasks: result.Source.Tasks}
	return map[string]any{
		"item":          item,
		"digest":        result.Plan.Digest,
		"target_exists": result.Plan.Targets[0].Exists,
		"diff":          result.Diff,
		"changed":       result.Plan.Applied,
		"replayed":      result.Plan.Replayed,
		"safety_backup": result.Plan.SafetyBackup,
	}, nil
}
