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
		return "", directoryError(err)
	}
	return filepath.Join(dirs.Data, "profiles"), nil
}

func directoryError(cause error) *protocol.Error {
	return protocol.WithStorageDetails(protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil), "paths", "resolve_user_directories", "user_directories", cause)
}

func (a *App) store(options map[string]string) (values.ModernStore, *protocol.Error) {
	p, err := project.Resolve(".", options["profile"])
	if err != nil {
		return values.ModernStore{}, err
	}
	dir, err := a.dataDirectory()
	if err != nil {
		return values.ModernStore{}, err
	}
	store := values.CurrentStore(dir, p.Profile)
	store.Invocation = protocol.InvocationContext{Profile: p.Profile, ProfileKnown: true, Env: options["env"], EnvKnown: true}
	if dirs, e := a.currentPaths(); e == nil {
		store.Invocation.IsolatedRoot = dirs.IsolatedRoot
	}
	return store, nil
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
	return object(map[string]any{"profile": stringSchema(), "changed": map[string]any{"type": "boolean"}, "warnings": warningsSchema()}, "profile", "changed")
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
					command.Options = append(command.Options, Option{Name: "value", Description: "Literal variable value; empty strings are allowed."})
				}
				command.Options = append(command.Options, Option{Name: "file", Description: "Read a UTF-8 " + string(kind) + " from this file, preserving whitespace.", MinLength: 1}, Option{Name: "stdin", Description: "Read a UTF-8 " + string(kind) + " from piped stdin, preserving whitespace.", Boolean: true})
				command.InputOneOf = []map[string]any{
					{"required": []string{"file"}, "properties": map[string]any{"stdin": map[string]any{"const": false}}},
					{"required": []string{"stdin"}, "properties": map[string]any{"stdin": map[string]any{"const": true}}, "not": map[string]any{"required": []string{"file"}}},
				}
				if kind == values.Variable {
					command.InputOneOf[0]["not"] = map[string]any{"required": []string{"value"}}
					command.InputOneOf[1]["not"] = map[string]any{"anyOf": []any{map[string]any{"required": []string{"file"}}, map[string]any{"required": []string{"value"}}}}
					command.InputOneOf = append(command.InputOneOf, map[string]any{"required": []string{"value"}, "properties": map[string]any{"stdin": map[string]any{"const": false}}, "not": map[string]any{"required": []string{"file"}}})
				}
			}
			if action == "set" || action == "unset" {
				guardMutation(&command, action == "set")
			}
			if action == "list" {
				command.Output = object(map[string]any{"profile": stringSchema(), "items": map[string]any{"type": "array", "items": metadataSchema()}, "integrity_verified": map[string]any{"type": "boolean"}}, "profile", "items", "integrity_verified")
			}
			if action == "get" {
				command.Output = object(map[string]any{"profile": stringSchema(), "value": stringSchema(), "metadata": metadataSchema(), "integrity_verified": map[string]any{"type": "boolean"}}, "profile", "value", "metadata", "integrity_verified")
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
			guardMutation(&command, false)
		} else {
			command.Output = object(map[string]any{"profile": stringSchema(), "items": map[string]any{"type": "array", "items": stringSchema()}}, "profile", "items")
		}
		command.Run = func(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
			guarded, err := mutationGuard(request.Options)
			if err != nil {
				return nil, err
			}
			store, err := a.store(request.Options)
			if err != nil {
				return nil, err
			}
			if action == "list" {
				state, err := store.ReadForQuery(ctx)
				if err != nil {
					a.scopeQueryRemedy(err, "")
					return nil, err
				}
				return map[string]any{"profile": store.Profile, "items": state.EnvNames()}, nil
			}
			if guarded {
				return store.Apply(ctx, values.Change{Action: "env." + action, Env: request.Args[0], Revision: request.Options["revision"], RequestID: request.Options["request-id"]})
			}
			changed, err := store.Update(ctx, func(state *values.Editor) (bool, *protocol.Error) {
				if action == "create" {
					return state.CreateEnv(request.Args[0])
				}
				return state.RemoveEnv(request.Args[0])
			})
			return changedResult(store.Profile, changed), err
		}
		a.commands = append(a.commands, command)
	}
	a.registerValueManagement()
}

func (a *App) valueCommand(ctx context.Context, streams IO, request Request, kind values.Kind, action string) (any, *protocol.Error) {
	guarded, err := mutationGuard(request.Options)
	if err != nil {
		return nil, err
	}
	store, err := a.store(request.Options)
	if err != nil {
		return nil, err
	}
	env := request.Options["env"]
	if action == "list" || action == "get" {
		state, err := store.ReadForQuery(ctx)
		if err != nil {
			a.scopeQueryRemedy(err, env)
			return nil, err
		}
		if action == "list" {
			items, err := state.List(kind, env)
			return map[string]any{"profile": store.Profile, "items": items, "integrity_verified": false}, err
		}
		value, metadata, err := state.GetVariable(request.Args[0], env)
		return map[string]any{"profile": store.Profile, "value": value, "metadata": metadata, "integrity_verified": false}, err
	}
	value, literal := request.Options["value"]
	if action == "set" {
		_, fromFile := request.Options["file"]
		fromStdin := request.Options["stdin"] == "true"
		inputs := 0
		for _, present := range []bool{literal, fromFile, fromStdin} {
			if present {
				inputs++
			}
		}
		if inputs != 1 {
			if kind == values.Variable {
				return nil, argumentError("Select exactly one variable input: --value, --file or --stdin.", "")
			}
			return nil, argumentError("Select exactly one secret input: --file or --stdin.", "")
		}
	}
	if action == "set" && !literal {
		// Check scope before consuming a potentially blocking input stream.
		if !guarded {
			state, err := store.Read(ctx)
			if err != nil {
				return nil, err
			}
			if err := state.CheckEnv(env); err != nil {
				return nil, err
			}
		}
		if kind == values.Secret {
			if err = store.RequireSecretWrite(ctx); err != nil {
				return nil, err
			}
		}
		value, err = readValueInput(ctx, streams, request.Options, string(kind))
		if err != nil {
			return nil, err
		}
	}
	if guarded {
		change := values.Change{Action: string(kind) + "." + action, Key: request.Args[0], Env: env, Revision: request.Options["revision"], RequestID: request.Options["request-id"], CreateOnly: request.Options["create-only"] == "true"}
		if action == "set" {
			change.Value = &value
		}
		return store.Apply(ctx, change)
	}
	changed, err := store.Update(ctx, func(state *values.Editor) (bool, *protocol.Error) {
		if action == "set" {
			return state.Set(kind, request.Args[0], env, value)
		}
		return state.Unset(kind, request.Args[0], env)
	})
	result := changedResult(store.Profile, changed)
	if err == nil && action == "set" && kind == values.Secret {
		if warning := values.FrontendSecretWarning(request.Args[0]); warning != nil {
			result["warnings"] = []values.Warning{*warning}
		}
	}
	return result, err
}
func warningsSchema() map[string]any {
	return map[string]any{"type": "array", "items": object(map[string]any{"code": stringSchema(), "key": stringSchema(), "message": stringSchema()}, "code", "key", "message")}
}

func readValueInput(ctx context.Context, streams IO, options map[string]string, role string) (string, *protocol.Error) {
	filePath, fromFile := options["file"]
	fromStdin := options["stdin"] == "true"
	if fromFile == fromStdin {
		return "", argumentError("Select exactly one "+role+" input: --file or --stdin.", "")
	}
	var reader io.Reader
	if fromFile {
		file, err := os.OpenFile(filePath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return "", protocol.InputError(role, "open_input", err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", argumentError("File input requires a regular file.", "file")
		}
		reader = file
	} else {
		reader = streams.In
		if reader == nil {
			return "", argumentError("Provide "+role+" data through piped stdin.", "stdin")
		}
		if file, ok := reader.(*os.File); ok {
			info, err := file.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice != 0 {
				return "", argumentError("Provide "+role+" data through a pipe or file redirect.", "stdin")
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
			return "", protocol.InputError(role, "read_input", read.err)
		}
		return string(read.data), nil
	}
}
