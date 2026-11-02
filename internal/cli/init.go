package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/profiles"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/term"
)

const profileRules = "Use 1–128 ASCII letters, digits, dots, underscores, or hyphens; start with a letter or digit.\n"

func initTerminals(streams IO) bool {
	for _, stream := range []any{streams.In, streams.Out, streams.Err} {
		file, ok := stream.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			return false
		}
	}
	return true
}

func initInputError(code, message string, exit int) *protocol.Error {
	return protocol.NewError(code, message, exit, map[string]any{"operation": "init", "phase": "validate", "effects": "none", "retry": "after_input_change", "field": "profile"})
}

func initCollision(profile string) *protocol.Error {
	return protocol.NewError("profile_name_in_use", fmt.Sprintf("Profile %q already exists; no configuration was created.", profile), 3, map[string]any{
		"phase": "validate", "reason": "profile_name_in_use", "effects": "none", "retry": "after_input_change", "target": map[string]any{"kind": "profile", "name": profile},
		"remedies": []protocol.Remedy{
			{Relation: protocol.SameStoreAction, Argv: []string{"devtools", "init", "--profile", profile}, Effect: "config_write", RequiredInputs: []string{}, Message: "Explicitly select this profile to share its values, secrets, and tasks."},
			{Relation: protocol.SameStoreAction, Argv: []string{"devtools", "init"}, Effect: "config_write", RequiredInputs: []string{"profile"}, Message: "Provide --profile with a different valid name."},
		},
	})
}

func (a *App) initProject(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
	return a.initDirectory(ctx, streams, r, ".")
}

func (a *App) initDirectory(ctx context.Context, streams IO, r Request, directory string) (any, *protocol.Error) {
	forced, disabled := r.Options["interactive"] == "true", r.Options["no-interactive"] == "true"
	_, hasInteractive := r.Options["interactive"]
	_, hasNoninteractive := r.Options["no-interactive"]
	if hasInteractive && hasNoninteractive {
		return nil, argumentError("Choose either --interactive or --no-interactive.", "interactive")
	}
	terminal := initTerminals(streams) && r.OutputFormat == presentation.FormatText
	if forced && !terminal {
		return nil, argumentError("Interactive init requires terminal input, output, error, and text format.", "interactive")
	}
	ci := strings.ToLower(os.Getenv("CI"))
	interactive := !disabled && terminal && (forced || !r.Agent && ci != "1" && ci != "true")
	_, explicit := r.Options["profile"]
	selected, failure := project.InspectInit(directory, r.Options["profile"], explicit)
	if failure != nil {
		return nil, failure
	}
	result := selected
	if !selected.Existing && explicit {
		result, failure = project.PublishInit(directory, selected, true)
	} else if !selected.Existing {
		directories, err := a.currentPaths()
		if err != nil {
			return nil, protocol.WithStorageDetails(initInputError("storage_error", "Cannot resolve profile storage.", 1), "profiles", "resolve_paths", "profile_store", err)
		}
		catalog := profiles.Catalog{Data: directories.Data}
		share := false
		invalid := 0
		for {
			if !project.ValidProfile(selected.Profile) {
				if !interactive {
					return nil, initInputError("invalid_argument", "The directory name cannot be used as a profile name. Provide --profile explicitly.", 2)
				}
				message := ""
				if selected.ProfileSource == "directory" {
					message = "The directory name cannot be used as a profile name.\n" + profileRules
				} else if invalid > 0 {
					message = "Invalid profile name.\n" + profileRules
				}
				if _, err := io.WriteString(streams.Err, message); err != nil {
					return nil, initInputError("io_error", "Cannot write profile prompt.", 1)
				}
			}
			if !project.ValidProfile(selected.Profile) || selected.ProfileSource == "prompt" && selected.Profile == "" {
				value, e := readProfileLine(ctx, streams, "Profile name: ")
				if e != nil {
					return nil, e
				}
				selected.Profile, selected.ProfileSource = value, "prompt"
				if !project.ValidProfile(value) {
					invalid++
					if invalid >= 3 {
						return nil, initInputError("invalid_argument", "Three invalid profile names were entered.", 2)
					}
					continue
				}
				invalid = 0
			}
			collision := false
			failure = catalog.WithPresence(ctx, selected.Profile, func(present bool) *protocol.Error {
				if present && !share {
					collision = true
					return nil
				}
				selected.ProfilePresence = "absent"
				if present {
					selected.ProfilePresence = "existing"
				}
				result, failure = project.PublishInit(directory, selected, false)
				return failure
			})
			if failure != nil || !collision {
				break
			}
			if !interactive {
				return nil, initCollision(selected.Profile)
			}
			prompt := fmt.Sprintf("Profile %q already exists.\nProjects using this name share values, secrets, and tasks.\n1) Use existing profile %q\n2) Choose another profile name\n3) Cancel\nChoice [3]: ", selected.Profile, selected.Profile)
			choice, e := readProfileLine(ctx, streams, prompt)
			if e != nil {
				return nil, e
			}
			switch choice {
			case "1":
				share = true
			case "2":
				selected.Profile, selected.ProfileSource, share = "", "prompt", false
			case "", "3":
				return nil, initInputError("canceled", "Project initialization canceled.", 130)
			default:
				invalid++
				if invalid >= 3 {
					return nil, initInputError("invalid_argument", "Three invalid choices were entered.", 2)
				}
			}
		}
	}
	if failure != nil {
		return nil, failure
	}
	return map[string]any{"changed": result.Created, "item": map[string]string{"config_path": result.ConfigPath, "profile": result.Profile, "profile_source": result.ProfileSource, "profile_presence": result.ProfilePresence}}, nil
}
