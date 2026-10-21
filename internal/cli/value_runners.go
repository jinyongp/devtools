package cli

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/valuehandoff"
	"github.com/jinyongp/devtools/internal/values"
)

func runnerSchema() map[string]any {
	return object(map[string]any{"runner_id": stringSchema(), "instance_id": stringSchema(), "protocol": map[string]any{"const": 1}, "redaction_protocol": map[string]any{"const": 1}, "enabled": map[string]any{"type": "boolean"}}, "runner_id", "instance_id", "protocol", "redaction_protocol", "enabled")
}
func (a *App) runnerStore() (valuehandoff.Store, *protocol.Error) {
	dir, e := a.dataDirectory()
	if e != nil {
		return valuehandoff.Store{}, e
	}
	root, err := valuehandoff.CanonicalRoot(filepath.Dir(dir))
	if err != nil {
		return valuehandoff.Store{}, values.VaultError(context.Background(), err)
	}
	return valuehandoff.Store{Root: root}, nil
}
func (a *App) registerValueRunners() {
	id := Argument{Name: "runner-id", Required: true, Pattern: valuehandoff.UUIDPattern}
	// Registrations and their request namespace belong to the root, not a
	// selected profile. Do not accept a profile flag that would be ignored.
	rootInputOptions := []Option{{Name: "request-id", Required: true, Pattern: valuehandoff.UUIDPattern, Description: "Canonical lowercase root-wide retry UUID."}, {Name: "file", Description: "Strict JSON registration body file."}, {Name: "stdin", Boolean: true, Description: "Read a strict JSON registration body from a pipe."}}
	a.commands = append(a.commands, Command{Name: "values runner address", Description: "Compute the same-user runner endpoint; this metadata grants no execution permission.", Arguments: []Argument{id}, Options: []Option{{Name: "instance", Required: true, Pattern: valuehandoff.UUIDPattern, Description: "Canonical lowercase instance UUID."}}, Output: object(map[string]any{"root_id": stringSchema(), "vault_id": stringSchema(), "runner_id": stringSchema(), "instance_id": stringSchema(), "endpoint": stringSchema(), "protocol": map[string]any{"const": 1}, "redaction_protocol": map[string]any{"const": 1}}, "root_id", "vault_id", "runner_id", "instance_id", "endpoint", "protocol", "redaction_protocol"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		s, e := a.runnerStore()
		if e != nil {
			return nil, e
		}
		address, err := valuehandoff.Address(s.Root, r.Args[0], r.Options["instance"])
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
		return address, nil
	}})
	output := object(map[string]any{"profile": stringSchema(), "item": runnerSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "profile", "item", "changed", "replayed")
	a.commands = append(a.commands, Command{Name: "values runner register", Description: "Probe and register an explicit live runner instance with a private retry receipt.", Options: rootInputOptions, InputOneOf: inputSourceSchema(), BodySchema: object(map[string]any{"runner_id": map[string]any{"type": "string", "pattern": valuehandoff.UUIDPattern}, "instance_id": map[string]any{"type": "string", "pattern": valuehandoff.UUIDPattern}, "protocol": map[string]any{"const": 1}, "redaction_protocol": map[string]any{"const": 1}}, "runner_id", "instance_id", "protocol", "redaction_protocol"), Output: output, Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		var input valuehandoff.RunnerInput
		if e := executionInput(ctx, streams, r, &input); e != nil {
			return nil, e
		}
		s, e := a.runnerStore()
		if e != nil {
			return nil, e
		}
		return s.Register(ctx, input, r.Options["request-id"])
	}})
	a.commands = append(a.commands, Command{Name: "values runner unregister", Description: "Disable only the observed instance; delivered inputs and external jobs cannot be revoked here.", Arguments: []Argument{id}, Options: []Option{{Name: "instance", Required: true, Pattern: valuehandoff.UUIDPattern, Description: "The observed instance to disable."}, {Name: "request-id", Required: true, Pattern: valuehandoff.UUIDPattern, Description: "Canonical lowercase retry UUID."}}, Output: output, Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		s, e := a.runnerStore()
		if e != nil {
			return nil, e
		}
		return s.Unregister(ctx, r.Args[0], r.Options["instance"], r.Options["request-id"])
	}})
	a.commands = append(a.commands, Command{Name: "values runner show", Description: "Passively authenticate the root-wide registration; does not probe or unlock.", Arguments: []Argument{id}, Output: object(map[string]any{"profile": stringSchema(), "item": runnerSchema()}, "profile", "item"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		s, e := a.runnerStore()
		if e != nil {
			return nil, e
		}
		item, e := s.Runner(ctx, r.Args[0])
		return map[string]any{"profile": "", "item": item}, e
	}})
}
