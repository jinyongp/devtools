package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (a *App) registerBackup() {
	pathOption := func(name string, required bool) Option {
		return Option{Name: name, Required: required, MinLength: 1, Description: "Filesystem path."}
	}
	profileOption := func(name string, required bool) Option {
		return Option{Name: name, Required: required, Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128, Description: "Profile identifier."}
	}
	add := func(name, description string, opts []Option, run func(context.Context, backup.Engine, Request) (any, *protocol.Error)) {
		boolean := map[string]any{"type": "boolean"}
		profiles := map[string]any{"type": "array", "items": object(map[string]any{"profile": stringSchema(), "values": boolean, "tasks": boolean}, "profile", "values", "tasks")}
		output := object(map[string]any{"created_at": stringSchema(), "profiles": profiles}, "created_at", "profiles")
		switch name {
		case "keygen":
			output = object(map[string]any{"identity_file": stringSchema(), "recipient_file": stringSchema()}, "identity_file", "recipient_file")
		case "configure":
			output = object(map[string]any{"directory": stringSchema(), "recipient": stringSchema()}, "directory", "recipient")
		case "create":
			output = object(map[string]any{"path": stringSchema(), "created_at": stringSchema(), "profiles": profiles}, "path", "created_at", "profiles")
		case "restore":
			output = object(map[string]any{"digest": stringSchema(), "applied": boolean, "safety_backup": stringSchema(), "targets": map[string]any{"type": "array", "items": object(map[string]any{"source": stringSchema(), "profile": stringSchema(), "exists": boolean}, "source", "profile", "exists")}}, "digest", "applied", "targets")
		}
		a.commands = append(a.commands, Command{Name: "backup " + name, Description: description, Options: opts, Output: output, Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			dirs, err := paths.Current()
			if err != nil {
				return nil, argumentError("Cannot resolve user directories.", "")
			}
			d, e := a.dataDirectory()
			if e != nil {
				return nil, e
			}
			return run(ctx, backup.Engine{Data: filepath.Dir(d), Config: dirs.Config, Cache: dirs.Cache}, r)
		}})
	}
	add("keygen", "Generate private identity and public recipient files without printing key material.", []Option{pathOption("identity-file", true), pathOption("recipient-file", true)}, func(_ context.Context, _ backup.Engine, r Request) (any, *protocol.Error) {
		return backup.Keygen(r.Options["identity-file"], r.Options["recipient-file"])
	})
	add("configure", "Store the default backup directory and public recipient.", []Option{pathOption("directory", true), pathOption("recipient-file", true)}, func(_ context.Context, e backup.Engine, r Request) (any, *protocol.Error) {
		return e.Configure(r.Options["directory"], r.Options["recipient-file"])
	})
	add("create", "Encrypt all profiles or one selected profile.", []Option{profileOption("profile", false), pathOption("output", false), pathOption("recipient-file", false)}, func(ctx context.Context, e backup.Engine, r Request) (any, *protocol.Error) {
		return e.Create(ctx, r.Options["profile"], r.Options["output"], r.Options["recipient-file"])
	})
	inputs := []Option{pathOption("file", true), pathOption("identity-file", true)}
	add("inspect", "Authenticate and inspect backup metadata without returning values.", inputs, func(_ context.Context, _ backup.Engine, r Request) (any, *protocol.Error) {
		return backup.Inspect(r.Options["file"], r.Options["identity-file"])
	})
	opts := append(append([]Option{}, inputs...), profileOption("profile", true), profileOption("as", false), Option{Name: "replace", Boolean: true, Description: "Permit whole-profile replacement after a safety backup."}, Option{Name: "apply", MinLength: 64, MaxLength: 64, Pattern: `^[0-9a-f]+$`, Description: "Apply the exact digest returned by preview."}, Option{Name: "request-id", Pattern: `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, Description: "UUID required when applying; reuse it for retries."})
	add("restore", "Preview a profile restore; apply requires the preview digest and request ID.", opts, func(ctx context.Context, e backup.Engine, r Request) (any, *protocol.Error) {
		if (r.Options["apply"] != "") != (r.Options["request-id"] != "") {
			return nil, argumentError("Apply and request-id must be supplied together.", "")
		}
		return e.Restore(ctx, r.Options["file"], r.Options["identity-file"], r.Options["profile"], r.Options["as"], r.Options["apply"], r.Options["request-id"], r.Options["replace"] == "true")
	})
}
