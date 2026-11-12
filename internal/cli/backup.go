package cli

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/archivekey"
	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func (a *App) backupEngine() (backup.Engine, *protocol.Error) {
	dirs, err := a.currentPaths()
	if err != nil {
		return backup.Engine{}, argumentError("Cannot resolve user directories.", "")
	}
	d, failure := a.dataDirectory()
	if failure != nil {
		return backup.Engine{}, failure
	}
	return backup.Engine{Data: filepath.Dir(d), Config: dirs.Config, Cache: dirs.Cache, VaultMaster: values.SessionMaster(filepath.Dir(d))}, nil
}

func filePathOption(name string, required bool) Option {
	return Option{Name: name, Required: required, MinLength: 1, Description: "Filesystem path."}
}

func profileIdentifierOption(name string, required bool) Option {
	return Option{Name: name, Required: required, Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128, Description: "Profile identifier."}
}

func publicRecipientOption() Option {
	return Option{Name: "recipient", MinLength: 1, Description: "Public age recipient. New keys use ML-KEM-768 + X25519."}
}

func backupPassphraseOptions() []Option {
	return []Option{
		{Name: "passphrase-file", MinLength: 1, Description: "Read the backup passphrase from a regular file."},
		{Name: "passphrase-stdin", Boolean: true, Description: "Read the backup passphrase from piped stdin."},
	}
}

func (a *App) registerBackup() {
	add := func(name, description string, opts []Option, run func(context.Context, IO, backup.Engine, Request) (any, *protocol.Error)) {
		boolean := map[string]any{"type": "boolean"}
		profiles := map[string]any{"type": "array", "items": object(map[string]any{"profile": stringSchema(), "values": boolean, "tasks": boolean}, "profile", "values", "tasks")}
		output := object(map[string]any{"created_at": stringSchema(), "profiles": profiles}, "created_at", "profiles")
		switch name {
		case "keygen":
			output = object(map[string]any{"identity_file": stringSchema(), "recipient_file": stringSchema()}, "identity_file", "recipient_file")
		case "configure":
			output = object(map[string]any{"directory": stringSchema(), "recipient": stringSchema()}, "directory", "recipient")
		case "status":
			output = object(map[string]any{"configured": boolean, "directory": stringSchema(), "recipient": stringSchema()}, "configured", "directory", "recipient")
		case "create":
			output = object(map[string]any{"path": stringSchema(), "created_at": stringSchema(), "profiles": profiles}, "path", "created_at", "profiles")
		case "restore":
			output = object(map[string]any{"digest": stringSchema(), "applied": boolean, "safety_backup": stringSchema(), "targets": map[string]any{"type": "array", "items": object(map[string]any{"source": stringSchema(), "profile": stringSchema(), "exists": boolean}, "source", "profile", "exists")}}, "digest", "applied", "targets")
		}
		if name == "inspect" || name == "status" {
			output = itemOutput(output)
		} else if name != "restore" {
			output = changedItemOutput(output)
		} else {
			fields := output["properties"].(map[string]any)
			fields["changed"], fields["replayed"] = boolean, boolean
			output["required"] = append(output["required"].([]string), "changed", "replayed")
		}
		a.commands = append(a.commands, Command{Name: "backup " + name, Description: description, Options: opts, Output: output, Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			engine, failure := a.backupEngine()
			if failure != nil {
				return nil, failure
			}
			result, failure := run(ctx, streams, engine, r)
			if failure != nil || name == "restore" {
				return result, failure
			}
			if name == "inspect" || name == "status" {
				return map[string]any{"item": result}, nil
			}
			return map[string]any{"item": result, "changed": true}, nil
		}})
	}
	add("keygen", "Generate an age ML-KEM-768 + X25519 key pair without printing key material.", []Option{filePathOption("identity-file", true), filePathOption("recipient-file", true)}, func(_ context.Context, _ IO, _ backup.Engine, r Request) (any, *protocol.Error) {
		return backup.Keygen(r.Options["identity-file"], r.Options["recipient-file"])
	})
	add("configure", "Store the default backup directory and hybrid public recipient.", []Option{filePathOption("directory", true), filePathOption("recipient-file", true)}, func(_ context.Context, _ IO, e backup.Engine, r Request) (any, *protocol.Error) {
		return e.Configure(r.Options["directory"], r.Options["recipient-file"])
	})
	add("status", "Show configured backup directory and public recipient without private identity material.", []Option{}, func(_ context.Context, _ IO, e backup.Engine, _ Request) (any, *protocol.Error) {
		return e.Status()
	})
	createOptions := append([]Option{profileIdentifierOption("profile", false), filePathOption("output", false), filePathOption("recipient-file", false), publicRecipientOption()}, backupPassphraseOptions()...)
	add("create", "Encrypt profiles with a passphrase or a hybrid public recipient.", createOptions, a.createBackup)
	a.commands[len(a.commands)-1].InputOneOf = exclusiveInputAlternatives("recipient", "recipient-file", "passphrase-file", "passphrase-stdin")
	inputs := append([]Option{filePathOption("file", true), filePathOption("identity-file", false)}, backupPassphraseOptions()...)
	add("inspect", "Authenticate and inspect backup metadata without returning values.", inputs, func(ctx context.Context, streams IO, _ backup.Engine, r Request) (any, *protocol.Error) {
		options, err := a.backupReadOptions(ctx, streams, r, "inspect")
		if err != nil {
			return nil, err
		}
		return backup.InspectArchive(options)
	})
	a.commands[len(a.commands)-1].InputOneOf = exclusiveInputAlternatives("identity-file", "passphrase-file", "passphrase-stdin")
	opts := append(append([]Option{}, inputs...), profileIdentifierOption("profile", true), profileIdentifierOption("as", false), Option{Name: "replace", Boolean: true, Description: "Permit whole-profile replacement after a safety backup."}, Option{Name: "apply", MinLength: 64, MaxLength: 64, Pattern: `^[0-9a-f]+$`, Description: "Apply the exact digest returned by preview."}, requestIDOption(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, supplied("apply")))
	add("restore", "Preview a profile restore; apply requires the preview digest.", opts, func(ctx context.Context, streams IO, e backup.Engine, r Request) (any, *protocol.Error) {
		options, failure := a.backupReadOptions(ctx, streams, r, "restore")
		if failure != nil {
			return nil, failure
		}
		plan, err := e.RestoreArchive(ctx, backup.RestoreOptions{
			ReadOptions: options, Source: r.Options["profile"], Target: r.Options["as"],
			Expected: r.Options["apply"], RequestID: r.Options["request-id"], Replace: r.Options["replace"] == "true",
		})
		return struct {
			backup.Plan
			Changed  bool `json:"changed"`
			Replayed bool `json:"replayed"`
		}{plan, plan.Applied, plan.Replayed}, err
	})
	a.commands[len(a.commands)-1].InputOneOf = exclusiveInputAlternatives("identity-file", "passphrase-file", "passphrase-stdin")
}

func backupRetryArgv(r Request, action string) []string {
	argv := []string{"devtools", "backup", action}
	for _, name := range []string{"file", "profile", "output", "as", "apply", "request-id"} {
		if value := r.Options[name]; value != "" {
			argv = append(argv, "--"+name, value)
		}
	}
	if r.Options["replace"] == "true" {
		argv = append(argv, "--replace")
	}
	return argv
}

func (a *App) backupPassphrase(ctx context.Context, streams IO, r Request, action string, confirm bool) (string, *protocol.Error) {
	prompt := a.passphrasePrompt
	if r.Agent {
		prompt = nil
	}
	return acquireArchivePassphrase(ctx, streams, r.Options, "Backup", backupRetryArgv(r, action), confirm, prompt)
}

func backupHasPassphrase(r Request) bool {
	return r.Options["passphrase-file"] != "" || r.Options["passphrase-stdin"] == "true"
}

func (a *App) createBackup(ctx context.Context, streams IO, e backup.Engine, r Request) (any, *protocol.Error) {
	if err := passphraseOptionConflict(r.Options, "recipient", "recipient-file"); err != nil {
		return nil, err
	}
	options := backup.CreateOptions{Profile: r.Options["profile"], Output: r.Options["output"], RecipientPath: r.Options["recipient-file"], Recipient: r.Options["recipient"]}
	if options.Recipient != "" || options.RecipientPath != "" {
		return e.CreateWithRecipient(ctx, options)
	}
	legacy := false
	if !backupHasPassphrase(r) {
		config, err := e.Status()
		if err != nil {
			return nil, err
		}
		if archivekey.IsHybridRecipient(config.Recipient) {
			// The engine rechecks the key before encryption if settings changed.
			return e.CreateWithRecipient(ctx, options)
		}
		legacy = config.Configured
	}
	passphrase, err := a.backupPassphrase(ctx, streams, r, "create", true)
	if err != nil {
		if legacy && err.Code == "passphrase_required" {
			legacyErr := backup.LegacyRecipientError()
			legacyErr.Details = err.Details
			return nil, legacyErr
		}
		return nil, err
	}
	options.Passphrase = passphrase
	return e.CreateWithPassphrase(ctx, options)
}

func (a *App) backupReadOptions(ctx context.Context, streams IO, r Request, action string) (backup.ReadOptions, *protocol.Error) {
	options := backup.ReadOptions{Path: r.Options["file"], IdentityPath: r.Options["identity-file"]}
	if err := passphraseOptionConflict(r.Options, "identity-file"); err != nil {
		return options, err
	}
	if options.IdentityPath != "" {
		return options, nil
	}
	if !backupHasPassphrase(r) {
		format, err := backup.ArchiveEncryptionMode(options.Path)
		if err != nil {
			var inputErr *protocol.Error
			if errors.As(err, &inputErr) {
				return options, inputErr
			}
			return options, protocol.NewError("invalid_backup", "Cannot identify the backup encryption format. Verify the archive and preserve the original.", 3, nil)
		}
		if format != archivekey.Passphrase {
			return options, protocol.NewError("identity_required", "This backup requires its matching private identity file.", 3, map[string]any{
				"remedies": []protocol.Remedy{{
					Relation: protocol.SameStoreAction, Effect: "read",
					Argv:           append(backupRetryArgv(r, action), "--identity-file", "PATH"),
					RequiredInputs: []string{"identity-file"},
					Message:        "Supply the private identity that matches the archive's public recipient.",
				}},
			})
		}
	}
	passphrase, err := a.backupPassphrase(ctx, streams, r, action, false)
	options.Passphrase = passphrase
	return options, err
}
