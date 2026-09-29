package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/backup"
	profilecatalog "github.com/jinyongp/devtools/internal/profiles"
	"github.com/jinyongp/devtools/internal/profiletransfer"
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

func exclusiveInputAlternatives(names ...string) []map[string]any {
	required := make([]map[string]any, 0, len(names))
	for _, name := range names {
		required = append(required, map[string]any{"required": []string{name}})
	}
	out := []map[string]any{{"not": map[string]any{"anyOf": required}}}
	for _, name := range names {
		others := []map[string]any{}
		for _, other := range names {
			if other != name {
				others = append(others, map[string]any{"required": []string{other}})
			}
		}
		out = append(out, map[string]any{
			"required": []string{name},
			"not":      map[string]any{"anyOf": others},
		})
	}
	return out
}

func profileImportInputOneOf() []map[string]any {
	encryption := exclusiveInputAlternatives("identity-file", "passphrase-file", "passphrase-stdin")
	out := []map[string]any{}
	for _, mode := range encryption {
		out = append(out,
			map[string]any{"allOf": []map[string]any{
				mode,
				{"not": map[string]any{"anyOf": []map[string]any{{"required": []string{"apply"}}, {"required": []string{"request-id"}}}}},
			}},
			map[string]any{"allOf": []map[string]any{
				mode,
				{"required": []string{"apply", "request-id"}},
			}},
		)
	}
	return out
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
			Name:        "profile transfer prepare",
			Description: "Prepare a local X25519 identity for advanced recipient-based profile transfers.",
			Output: changedItemOutput(object(map[string]any{
				"recipient": stringSchema(),
			}, "recipient")),
			Run: a.prepareProfileTransfer,
		},
		Command{
			Name:        "profile export",
			Description: "Encrypt one profile into a single portable archive; prompts for a passphrase by default.",
			Options: []Option{
				profileIdentifierOption("profile", false),
				{Name: "dir", Default: ".", MinLength: 1, Description: "Project directory used when profile is omitted."},
				{Name: "output", MinLength: 1, Description: "Output file or existing directory; directories use <profile>.age and omission defaults to ./<profile>.age."},
				{Name: "recipient-file", MinLength: 1, Description: "Advanced mode: destination public age X25519 recipient file; mutually exclusive with passphrase input."},
				{Name: "recipient", MinLength: 1, Description: "Advanced mode: destination public age X25519 recipient; mutually exclusive with passphrase input."},
				{Name: "passphrase-file", MinLength: 1, Description: "Read the transfer passphrase from a regular file instead of prompting."},
				{Name: "passphrase-stdin", Boolean: true, Description: "Read the transfer passphrase from piped stdin instead of prompting."},
			},
			InputOneOf: exclusiveInputAlternatives("recipient", "recipient-file", "passphrase-file", "passphrase-stdin"),
			Output:     changedItemOutput(profileExportItemSchema()),
			Run:        a.exportProfile,
		},
		Command{
			Name:        "profile import",
			Description: "Preview an encrypted profile import; passphrase archives prompt by default and recipient archives use an identity.",
			Options: []Option{
				filePathOption("file", true),
				{Name: "identity-file", MinLength: 1, Description: "Advanced mode: private age X25519 identity file for recipient-encrypted archives."},
				{Name: "passphrase-file", MinLength: 1, Description: "Read the archive passphrase from a regular file instead of prompting."},
				{Name: "passphrase-stdin", Boolean: true, Description: "Read the archive passphrase from piped stdin instead of prompting."},
				profileIdentifierOption("profile", false),
				profileIdentifierOption("as", false),
				{Name: "replace", Boolean: true, Description: "Permit replacement of an existing target profile after creating a safety backup."},
				{Name: "apply", MinLength: 64, MaxLength: 64, Pattern: `^[0-9a-f]+$`, Description: "Apply the exact digest returned by preview."},
				{Name: "request-id", Pattern: uuidPattern, Description: "UUID required when applying; reuse it for retries."},
			},
			InputOneOf: profileImportInputOneOf(),
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

func (a *App) profileTransferStore() (profiletransfer.Store, *protocol.Error) {
	directory, err := a.dataDirectory()
	if err != nil {
		return profiletransfer.Store{}, err
	}
	return profiletransfer.Store{Data: filepath.Dir(directory)}, nil
}

func transferIdentityError(err error) *protocol.Error {
	switch {
	case errors.Is(err, profiletransfer.ErrIdentityMissing):
		return protocol.NewError("transfer_identity_missing", "No local profile transfer identity is prepared.", 3, map[string]any{
			"remedies": []protocol.Remedy{{
				Argv:           []string{"devtools", "profile", "transfer", "prepare"},
				RequiredInputs: []string{},
				Message:        "Prepare this destination installation, then re-export the source profile to the new public recipient before importing.",
			}},
		})
	case errors.Is(err, profiletransfer.ErrInvalidIdentity):
		return protocol.NewError("transfer_identity_invalid", "The local profile transfer identity is invalid or unsafe.", 3, map[string]any{
			"remedies": []protocol.Remedy{{
				Argv:           []string{"devtools", "profile", "import"},
				RequiredInputs: []string{"file", "identity-file"},
				Message:        "Use a matching existing private identity with --identity-file, or restore the local identity's private permissions and original contents. A new key cannot decrypt old archives.",
			}},
		})
	default:
		return protocol.NewError("storage_error", "Cannot access private profile transfer storage.", 1, nil)
	}
}

// Transfer shares the archive engine, but not the backup-configuration workflow.
func transferArchiveError(err *protocol.Error) *protocol.Error {
	remedy := protocol.Remedy{Argv: []string{}, RequiredInputs: []string{}}
	message := ""
	switch err.Code {
	case "invalid_recipient":
		message = "Supply a valid public age X25519 recipient for the destination device."
		remedy.Argv = []string{"devtools", "profile", "transfer", "prepare"}
		remedy.Message = "Run this on the destination device, then copy its public recipient into --recipient or --recipient-file on the source device."
	case "invalid_backup":
		message = "Cannot read or authenticate the profile archive with the selected passphrase or identity."
		remedy.Argv = []string{"devtools", "profile", "import"}
		remedy.RequiredInputs = []string{"file"}
		remedy.Message = "Check that the archive is complete. For the default transfer use the matching passphrase; for an advanced recipient archive use the matching --identity-file or prepared local identity."
	case "profile_exists":
		message = "The target profile already exists; replacement requires explicit authorization."
		remedy.Argv = []string{"devtools", "profile", "import"}
		remedy.RequiredInputs = []string{"file", "as"}
		remedy.Message = "Choose an unused target with --as, or review a fresh preview and supply --replace with its digest and a request ID to replace the existing profile."
	case "backup_error":
		message = "The profile archive operation could not be completed."
		remedy.Message = "Check archive paths and private storage permissions. Replacement requires a writable private recovery directory and a durable safety archive."
	default:
		return err
	}
	return protocol.NewError(err.Code, message, err.ExitCode, map[string]any{"remedies": []protocol.Remedy{remedy}})
}

func (a *App) prepareProfileTransfer(ctx context.Context, _ IO, _ Request) (any, *protocol.Error) {
	store, err := a.profileTransferStore()
	if err != nil {
		return nil, err
	}
	prepared, prepareErr := store.Prepare(ctx)
	if prepareErr != nil {
		if errors.Is(prepareErr, context.Canceled) || errors.Is(prepareErr, context.DeadlineExceeded) {
			return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		return nil, transferIdentityError(prepareErr)
	}
	return map[string]any{
		"item":    map[string]string{"recipient": prepared.Recipient},
		"changed": prepared.Changed,
	}, nil
}

func (a *App) listProfiles(ctx context.Context, _ IO, _ Request) (any, *protocol.Error) {
	catalog, err := a.profileCatalog()
	if err != nil {
		return nil, err
	}
	items, listErr := catalog.List(ctx)
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

func (a *App) diffProfiles(ctx context.Context, _ IO, request Request) (any, *protocol.Error) {
	catalog, err := a.profileCatalog()
	if err != nil {
		return nil, err
	}
	return catalog.Diff(ctx, request.Args[0], request.Args[1])
}

func profileExportOutputExists(path string) *protocol.Error {
	return protocol.NewError("output_exists", "Profile export output already exists; choose another --output path.", 3, map[string]any{
		"field": "output",
		"path":  path,
		"remedies": []protocol.Remedy{{
			Argv:           []string{},
			RequiredInputs: []string{"output"},
			Message:        "Retry profile export with a different --output path and the same destination recipient.",
		}},
	})
}

func resolveProfileExportOutput(profile, requested string) (string, *protocol.Error) {
	output := requested
	if output == "" {
		output = "./" + profile + ".age"
	} else {
		info, err := os.Stat(output)
		switch {
		case err == nil && info.IsDir():
			output = filepath.Join(output, profile+".age")
		case err == nil:
			return "", profileExportOutputExists(output)
		case !errors.Is(err, os.ErrNotExist):
			return "", protocol.NewError("backup_error", "Profile export output path could not be inspected.", 3, map[string]any{"field": "output", "path": output})
		}
	}
	if _, err := os.Lstat(output); err == nil {
		return "", profileExportOutputExists(output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", protocol.NewError("backup_error", "Profile export output path could not be inspected.", 3, map[string]any{"field": "output", "path": output})
	}
	return output, nil
}

func (a *App) exportProfile(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	recipient, recipientFile := request.Options["recipient"], request.Options["recipient-file"]
	if recipient != "" && recipientFile != "" {
		return nil, argumentError("Choose recipient or recipient-file.", "recipient")
	}
	if err := passphraseOptionConflict(request.Options, "recipient", "recipient-file"); err != nil {
		return nil, err
	}
	profile := request.Options["profile"]
	if profile == "" {
		p, err := project.Resolve(request.Options["dir"], "")
		if err != nil {
			return nil, err
		}
		profile = p.Profile
	}
	output, outputErr := resolveProfileExportOutput(profile, request.Options["output"])
	if outputErr != nil {
		return nil, outputErr
	}
	engine, err := a.backupEngine()
	if err != nil {
		return nil, err
	}

	var (
		result  backup.CreateResult
		failure *protocol.Error
	)
	if recipient != "" || recipientFile != "" {
		result, failure = engine.CreateWithRecipient(ctx, backup.CreateOptions{Profile: profile, Output: output, RecipientPath: recipientFile, Recipient: recipient})
	} else {
		passphrase, passphraseErr := acquireTransferPassphrase(ctx, streams, request.Options, "export", true, a.passphrasePrompt)
		if passphraseErr != nil {
			return nil, passphraseErr
		}
		result, failure = engine.CreateWithPassphrase(ctx, backup.CreateOptions{Profile: profile, Output: output, Passphrase: passphrase})
	}
	if failure != nil {
		if ctx.Err() != nil {
			return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		if failure.Code == "backup_error" && failure.Details != nil && failure.Details["cause"] == "output_exists" {
			return nil, profileExportOutputExists(output)
		}
		return nil, transferArchiveError(failure)
	}
	if len(result.Profiles) != 1 || result.Profiles[0].Profile != profile {
		return nil, protocol.NewError("internal_error", "Profile export did not produce exactly one profile.", 1, nil)
	}
	item := profileTransferItem{Profile: profile, Path: result.Path, CreatedAt: result.CreatedAt, Values: result.Profiles[0].Values, Tasks: result.Profiles[0].Tasks}
	return map[string]any{"item": item, "changed": true}, nil
}

func (a *App) importProfile(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
	if (request.Options["apply"] != "") != (request.Options["request-id"] != "") {
		return nil, argumentError("Apply and request-id must be supplied together.", "")
	}
	if err := passphraseOptionConflict(request.Options, "identity-file"); err != nil {
		return nil, err
	}
	transferStore, storeErr := a.profileTransferStore()
	if storeErr != nil {
		return nil, storeErr
	}

	identityPath := request.Options["identity-file"]
	passphrase := ""
	explicitPassphrase := request.Options["passphrase-file"] != "" || request.Options["passphrase-stdin"] == "true"
	switch {
	case identityPath != "":
		// Explicit advanced identity mode.
	case explicitPassphrase:
		var passphraseErr *protocol.Error
		passphrase, passphraseErr = acquireTransferPassphrase(ctx, streams, request.Options, "import", false, a.passphrasePrompt)
		if passphraseErr != nil {
			return nil, passphraseErr
		}
	default:
		mode, modeErr := backup.ArchiveEncryptionMode(request.Options["file"])
		if modeErr != nil {
			return nil, transferArchiveError(protocol.NewError("invalid_backup", "Cannot inspect the profile archive encryption mode.", 3, nil))
		}
		if mode == backup.ArchiveEncryptionPassphrase {
			var passphraseErr *protocol.Error
			passphrase, passphraseErr = acquireTransferPassphrase(ctx, streams, request.Options, "import", false, a.passphrasePrompt)
			if passphraseErr != nil {
				return nil, passphraseErr
			}
		} else {
			var identityErr error
			identityPath, identityErr = transferStore.ExistingIdentityPath()
			if identityErr != nil {
				return nil, transferIdentityError(identityErr)
			}
		}
	}

	engine, engineErr := a.backupEngine()
	if engineErr != nil {
		return nil, engineErr
	}
	result, importErr := engine.Import(ctx, backup.ImportRequest{
		Path:              request.Options["file"],
		IdentityPath:      identityPath,
		Passphrase:        passphrase,
		RecoveryDirectory: transferStore.RecoveryDirectory(),
		Source:            request.Options["profile"],
		Target:            request.Options["as"],
		Expected:          request.Options["apply"],
		RequestID:         request.Options["request-id"],
		Replace:           request.Options["replace"] == "true",
	})
	if importErr != nil {
		return nil, transferArchiveError(importErr)
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
