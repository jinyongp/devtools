package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) backupEngine() (backup.Engine, *protocol.Error) {
	dirs, err := paths.Current()
	if err != nil {
		return backup.Engine{}, argumentError("Cannot resolve user directories.", "")
	}
	d, failure := a.dataDirectory()
	if failure != nil {
		return backup.Engine{}, failure
	}
	return backup.Engine{Data: filepath.Dir(d), Config: dirs.Config, Cache: dirs.Cache}, nil
}

func filePathOption(name string, required bool) Option {
	return Option{Name: name, Required: required, MinLength: 1, Description: "Filesystem path."}
}

func profileIdentifierOption(name string, required bool) Option {
	return Option{Name: name, Required: required, Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128, Description: "Profile identifier."}
}

func publicRecipientOption() Option {
	return Option{Name: "recipient", MinLength: 1, Description: "Public age X25519 recipient."}
}

func recipientInputOneOf() []map[string]any {
	return []map[string]any{
		{"not": map[string]any{"anyOf": []map[string]any{{"required": []string{"recipient"}}, {"required": []string{"recipient-file"}}}}},
		{"required": []string{"recipient"}, "not": map[string]any{"required": []string{"recipient-file"}}},
		{"required": []string{"recipient-file"}, "not": map[string]any{"required": []string{"recipient"}}},
	}
}

func (a *App) registerBackup() {
	add := func(name, description string, opts []Option, run func(context.Context, backup.Engine, Request) (any, *protocol.Error)) {
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
		a.commands = append(a.commands, Command{Name: "backup " + name, Description: description, Options: opts, Output: output, Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			engine, failure := a.backupEngine()
			if failure != nil {
				return nil, failure
			}
			result, failure := run(ctx, engine, r)
			if failure != nil || name == "restore" {
				return result, failure
			}
			if name == "inspect" || name == "status" {
				return map[string]any{"item": result}, nil
			}
			return map[string]any{"item": result, "changed": true}, nil
		}})
	}
	add("keygen", "Generate private identity and public recipient files without printing key material.", []Option{filePathOption("identity-file", true), filePathOption("recipient-file", true)}, func(_ context.Context, _ backup.Engine, r Request) (any, *protocol.Error) {
		return backup.Keygen(r.Options["identity-file"], r.Options["recipient-file"])
	})
	add("configure", "Store the default backup directory and public recipient.", []Option{filePathOption("directory", true), filePathOption("recipient-file", true)}, func(_ context.Context, e backup.Engine, r Request) (any, *protocol.Error) {
		return e.Configure(r.Options["directory"], r.Options["recipient-file"])
	})
	add("status", "Show configured backup directory and public recipient without private identity material.", []Option{}, func(_ context.Context, e backup.Engine, _ Request) (any, *protocol.Error) {
		return e.Status()
	})
	add("create", "Encrypt all profiles or one selected profile.", []Option{profileIdentifierOption("profile", false), filePathOption("output", false), filePathOption("recipient-file", false), publicRecipientOption()}, func(ctx context.Context, e backup.Engine, r Request) (any, *protocol.Error) {
		return e.CreateWithRecipient(ctx, backup.CreateOptions{Profile: r.Options["profile"], Output: r.Options["output"], RecipientPath: r.Options["recipient-file"], Recipient: r.Options["recipient"]})
	})
	a.commands[len(a.commands)-1].InputOneOf = recipientInputOneOf()
	inputs := []Option{filePathOption("file", true), filePathOption("identity-file", true)}
	add("inspect", "Authenticate and inspect backup metadata without returning values.", inputs, func(_ context.Context, _ backup.Engine, r Request) (any, *protocol.Error) {
		return backup.Inspect(r.Options["file"], r.Options["identity-file"])
	})
	opts := append(append([]Option{}, inputs...), profileIdentifierOption("profile", true), profileIdentifierOption("as", false), Option{Name: "replace", Boolean: true, Description: "Permit whole-profile replacement after a safety backup."}, Option{Name: "apply", MinLength: 64, MaxLength: 64, Pattern: `^[0-9a-f]+$`, Description: "Apply the exact digest returned by preview."}, Option{Name: "request-id", Pattern: `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, Description: "UUID required when applying; reuse it for retries."})
	add("restore", "Preview a profile restore; apply requires the preview digest and request ID.", opts, func(ctx context.Context, e backup.Engine, r Request) (any, *protocol.Error) {
		if (r.Options["apply"] != "") != (r.Options["request-id"] != "") {
			return nil, argumentError("Apply and request-id must be supplied together.", "")
		}
		plan, err := e.Restore(ctx, r.Options["file"], r.Options["identity-file"], r.Options["profile"], r.Options["as"], r.Options["apply"], r.Options["request-id"], r.Options["replace"] == "true")
		return struct {
			backup.Plan
			Changed  bool `json:"changed"`
			Replayed bool `json:"replayed"`
		}{plan, plan.Applied, plan.Replayed}, err
	})
}
