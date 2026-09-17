package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func userDataDirectory() (string, *protocol.Error) {
	dirs, err := paths.Current()
	if err != nil {
		return "", protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
	}
	return filepath.Join(dirs.Data, "profiles"), nil
}

func (a *App) store(options map[string]string) (values.Store, *protocol.Error) {
	p, err := project.Resolve(".", options["profile"])
	if err != nil {
		return values.Store{}, err
	}
	dir, err := a.dataDirectory()
	if err != nil {
		return values.Store{}, err
	}
	return values.Store{Directory: dir, Profile: p.Profile}, nil
}

func profileOptions(withEnv bool) []Option {
	options := []Option{{Name: "profile", Description: "Project profile; defaults to devtools.toml.", Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128}}
	if withEnv {
		options = append(options, Option{Name: "env", Description: "Existing env; omission selects common values.", Pattern: project.ProfilePattern, MinLength: 1, MaxLength: 128})
	}
	return options
}

func metadataSchema() map[string]any {
	return object(map[string]any{"key": stringSchema(), "kind": map[string]any{"enum": []string{"variable", "secret"}}, "source": map[string]any{"enum": []string{"common", "env"}}, "overrides": map[string]any{"type": "boolean"}}, "key", "kind", "source", "overrides")
}

func changedSchema() map[string]any {
	return object(map[string]any{"profile": stringSchema(), "changed": map[string]any{"type": "boolean"}}, "profile", "changed")
}

func changedResult(profile string, changed bool) map[string]any {
	return map[string]any{"profile": profile, "changed": changed}
}

func (a *App) registerTools() {
	for _, kind := range []values.Kind{values.Variable, values.Secret} {
		alias := "var"
		if kind == values.Secret {
			alias = "sec"
		}
		actions := []string{"set", "list", "unset"}
		if kind == values.Variable {
			actions = append(actions, "get")
		}
		for _, action := range actions {
			command := Command{Name: string(kind) + " " + action, Aliases: []string{alias + " " + action}, Description: action + " " + string(kind) + " values in the selected profile scope.", Options: profileOptions(true), Output: changedSchema()}
			if action != "list" {
				command.Arguments = []Argument{{Name: "key", Required: true, Pattern: values.KeyPattern}}
			}
			if action == "set" {
				if kind == values.Variable {
					command.Options = append(command.Options, Option{Name: "value", Required: true, Description: "Literal variable value; empty strings are allowed."})
				} else {
					command.Options = append(command.Options, Option{Name: "file", Description: "Read a UTF-8 secret from this file, preserving whitespace.", MinLength: 1}, Option{Name: "stdin", Description: "Read a UTF-8 secret from piped stdin, preserving whitespace.", Boolean: true})
					command.InputOneOf = []map[string]any{
						{"required": []string{"file"}, "properties": map[string]any{"stdin": map[string]any{"const": false}}},
						{"required": []string{"stdin"}, "properties": map[string]any{"stdin": map[string]any{"const": true}}, "not": map[string]any{"required": []string{"file"}}},
					}
				}
			}
			if action == "list" {
				command.Output = object(map[string]any{"profile": stringSchema(), "items": map[string]any{"type": "array", "items": metadataSchema()}}, "profile", "items")
			}
			if action == "get" {
				command.Output = object(map[string]any{"profile": stringSchema(), "value": stringSchema(), "metadata": metadataSchema()}, "profile", "value", "metadata")
			}
			command.Run = func(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
				return a.valueCommand(ctx, streams, request, kind, action)
			}
			a.commands = append(a.commands, command)
		}
	}
	for _, action := range []string{"create", "list", "remove"} {
		command := Command{Name: "env " + action, Description: action + " environments in the selected profile.", Options: profileOptions(false), Output: changedSchema()}
		if action != "list" {
			command.Arguments = []Argument{{Name: "env", Required: true, Pattern: project.ProfilePattern}}
		} else {
			command.Output = object(map[string]any{"profile": stringSchema(), "envs": map[string]any{"type": "array", "items": stringSchema()}}, "profile", "envs")
		}
		command.Run = func(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
			store, err := a.store(request.Options)
			if err != nil {
				return nil, err
			}
			if action == "list" {
				state, err := store.Read()
				if err != nil {
					return nil, err
				}
				return map[string]any{"profile": store.Profile, "envs": state.EnvNames()}, nil
			}
			changed, err := store.Update(ctx, func(state *values.State) (bool, *protocol.Error) {
				if action == "create" {
					return state.CreateEnv(request.Args[0])
				}
				return state.RemoveEnv(request.Args[0])
			})
			return changedResult(store.Profile, changed), err
		}
		a.commands = append(a.commands, command)
	}
}

func (a *App) valueCommand(ctx context.Context, streams IO, request Request, kind values.Kind, action string) (any, *protocol.Error) {
	store, err := a.store(request.Options)
	if err != nil {
		return nil, err
	}
	env := request.Options["env"]
	if action == "list" || action == "get" {
		state, err := store.Read()
		if err != nil {
			return nil, err
		}
		if action == "list" {
			items, err := state.List(kind, env)
			return map[string]any{"profile": store.Profile, "items": items}, err
		}
		value, metadata, err := state.GetVariable(request.Args[0], env)
		return map[string]any{"profile": store.Profile, "value": value, "metadata": metadata}, err
	}
	value := request.Options["value"]
	if action == "set" && kind == values.Secret {
		// Check scope before consuming a potentially blocking input stream.
		state, err := store.Read()
		if err != nil {
			return nil, err
		}
		if err := state.CheckEnv(env); err != nil {
			return nil, err
		}
		value, err = readSecret(ctx, streams, request.Options)
		if err != nil {
			return nil, err
		}
	}
	changed, err := store.Update(ctx, func(state *values.State) (bool, *protocol.Error) {
		if action == "set" {
			return state.Set(kind, request.Args[0], env, value)
		}
		return state.Unset(kind, request.Args[0], env)
	})
	return changedResult(store.Profile, changed), err
}

func readSecret(ctx context.Context, streams IO, options map[string]string) (string, *protocol.Error) {
	filePath, fromFile := options["file"]
	fromStdin := options["stdin"] == "true"
	if fromFile == fromStdin {
		return "", argumentError("Select exactly one secret input: --file or --stdin.", "")
	}
	var reader io.Reader
	if fromFile {
		file, err := os.OpenFile(filePath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return "", protocol.NewError("io_error", "Cannot read secret input.", 1, nil)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", argumentError("Secret file input requires a regular file.", "file")
		}
		reader = file
	} else {
		reader = streams.In
		if reader == nil {
			return "", argumentError("Provide secret data through piped stdin.", "stdin")
		}
		if file, ok := reader.(*os.File); ok {
			info, err := file.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice != 0 {
				return "", argumentError("Provide secret data through a pipe or file redirect.", "stdin")
			}
		}
	}
	type readResult struct {
		data []byte
		err  error
	}
	result := make(chan readResult, 1)
	go func() { data, err := io.ReadAll(reader); result <- readResult{data, err} }()
	select {
	case <-ctx.Done():
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
		}
		return "", protocol.NewError("canceled", "Execution canceled.", 130, nil)
	case read := <-result:
		if read.err != nil {
			return "", protocol.NewError("io_error", "Cannot read secret input.", 1, nil)
		}
		return string(read.data), nil
	}
}
