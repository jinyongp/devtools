package cli

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/valuehandoff"
	"github.com/jinyongp/devtools/internal/values"
)

func executionSchema() map[string]any {
	p := map[string]any{}
	for _, k := range []string{"execution_id", "input_id", "external_execution_id", "created_at", "expires_at", "delivery_state", "execution_state", "runner_id", "runner_instance_id", "reporter_instance_id", "delivery_id", "external_ref", "reason", "fact_source"} {
		p[k] = stringSchema()
	}
	p["revision"] = map[string]any{"type": "integer", "minimum": 1}
	p["exit_code"] = map[string]any{"type": "integer"}
	return object(p, "execution_id", "input_id", "external_execution_id", "revision", "created_at", "expires_at", "delivery_state", "execution_state")
}
func executionResultSchema() map[string]any {
	return object(map[string]any{"profile": stringSchema(), "item": executionSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "profile", "item", "changed", "replayed")
}
func (a *App) executionStore(r Request) (valuehandoff.Store, *protocol.Error) {
	s, e := a.store(r.Options)
	if e != nil {
		return valuehandoff.Store{}, e
	}
	root, err := valuehandoff.CanonicalRoot(filepath.Dir(s.Directory))
	if err != nil {
		return valuehandoff.Store{}, values.VaultError(context.Background(), err)
	}
	return valuehandoff.Store{Root: root, Profile: s.Profile}, nil
}
func executionOptions() []Option {
	return append(profileOptions(false), Option{Name: "request-id", Required: true, Pattern: valuehandoff.UUIDPattern, Description: "Canonical lowercase operation UUID; retry identical input."}, Option{Name: "file", Description: "Strict JSON control body file; contains no secret values."}, Option{Name: "stdin", Boolean: true, Description: "Read strict JSON control body from a pipe."})
}
func executionInput(ctx context.Context, streams IO, r Request, out any) *protocol.Error {
	if (r.Options["file"] != "") == (r.Options["stdin"] == "true") {
		return argumentError("Choose exactly one of --file or --stdin.", "")
	}
	body, e := taskInput(ctx, streams, r)
	if e != nil {
		return e
	}
	if valuehandoff.DecodeControl([]byte(body), out) != nil {
		return argumentError("Provide a valid strict JSON execution control body.", "")
	}
	return nil
}
func inputSourceSchema() []map[string]any {
	return []map[string]any{{"required": []string{"file"}, "not": map[string]any{"required": []string{"stdin"}}}, {"required": []string{"stdin"}, "properties": map[string]any{"stdin": map[string]any{"const": true}}, "not": map[string]any{"required": []string{"file"}}}}
}
func (a *App) registerValueExecutions() {
	a.registerExecutionControls()
	a.registerExecutionDelivery()
	a.registerExecutionReport()
	selection := object(map[string]any{"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string", "pattern": `^[A-Za-z_][A-Za-z0-9_]*$`}, "uniqueItems": true, "maxItems": 5000}, "bundle": map[string]any{"type": "string", "pattern": project.ProfilePattern, "maxLength": 128}}, "keys")
	opaque := map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "pattern": `^[ -~]+$`}
	body := object(map[string]any{"external_execution_id": opaque, "selection": selection, "env": stringSchema()}, "external_execution_id", "selection")
	a.commands = append(a.commands, Command{Name: "values execution prepare", Description: "Freeze explicit scoped inputs for 30 seconds; selection does not approve delivery.", Options: executionOptions(), InputOneOf: inputSourceSchema(), BodySchema: body, Output: executionResultSchema(), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		var input valuehandoff.PrepareInput
		if e := executionInput(ctx, streams, r, &input); e != nil {
			return nil, e
		}
		s, e := a.executionStore(r)
		if e != nil {
			return nil, e
		}
		return s.Prepare(ctx, input, r.Options["request-id"])
	}})
	a.commands = append(a.commands, Command{Name: "values execution status", Description: "Passively authenticate current execution metadata; never deliver or start work.", Options: append(profileOptions(false), Option{Name: "request-id", Pattern: valuehandoff.UUIDPattern, Description: "Recover the execution selected by its prepare request UUID."}), Arguments: []Argument{{Name: "execution-id", Pattern: valuehandoff.IDPattern}}, Output: object(map[string]any{"profile": stringSchema(), "item": executionSchema()}, "profile", "item"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		s, e := a.executionStore(r)
		if e != nil {
			return nil, e
		}
		id := ""
		if len(r.Args) > 0 {
			id = r.Args[0]
		}
		return s.Status(ctx, id, r.Options["request-id"])
	}})
	a.commands = append(a.commands, Command{Name: "values execution list", Description: "Passively list verified external delivery metadata with revision-bound pagination.", Options: append(profileOptions(false), Option{Name: "limit", Pattern: `^[0-9]+$`, Description: "Page size, default 50, maximum 200."}, Option{Name: "cursor", Description: "Opaque cursor from the previous page."}), Output: object(map[string]any{"profile": stringSchema(), "items": arraySchema(executionSchema()), "next_cursor": map[string]any{"type": []string{"string", "null"}}}, "profile", "items", "next_cursor"), Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
		s, e := a.executionStore(r)
		if e != nil {
			return nil, e
		}
		limit := 0
		if r.Options["limit"] != "" {
			var err error
			limit, err = strconv.Atoi(r.Options["limit"])
			if err != nil || limit == 0 {
				return nil, argumentError("Use a page size from 1 through 200.", "limit")
			}
		}
		return s.List(ctx, limit, r.Options["cursor"])
	}})
}

func (a *App) registerExecutionReport() {
	opaque := map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "pattern": `^[ -~]+$`}
	body := object(map[string]any{
		"revision":              map[string]any{"type": "integer", "minimum": 1, "maximum": uint64(1<<64 - 1)},
		"runner_id":             map[string]any{"type": "string", "pattern": valuehandoff.UUIDPattern},
		"input_id":              map[string]any{"type": "string", "pattern": valuehandoff.IDPattern},
		"external_execution_id": opaque, "external_ref": opaque,
		"state":     map[string]any{"type": "string", "enum": []string{"started", "finished", "failed", "unknown"}},
		"exit_code": map[string]any{"type": "integer"},
		"reason":    map[string]any{"type": "string", "enum": []string{"runner_start_failed", "runner_lost", "execution_failed", "canceled", "outcome_unavailable"}},
	}, "revision", "runner_id", "external_execution_id", "input_id", "state")
	body["oneOf"] = []map[string]any{
		{"properties": map[string]any{"state": map[string]any{"const": "started"}}, "required": []string{"external_ref"}, "not": map[string]any{"anyOf": []map[string]any{{"required": []string{"exit_code"}}, {"required": []string{"reason"}}}}},
		{"properties": map[string]any{"state": map[string]any{"const": "finished"}}, "required": []string{"external_ref", "exit_code"}},
		{"properties": map[string]any{"state": map[string]any{"enum": []string{"failed", "unknown"}}}, "required": []string{"reason"}, "not": map[string]any{"required": []string{"exit_code"}}},
	}
	a.commands = append(a.commands, Command{Name: "values execution report", Description: "Record runner-reported engine facts without delivering or starting work.", Options: executionOptions(), Arguments: []Argument{{Name: "execution-id", Required: true, Pattern: valuehandoff.IDPattern}}, InputOneOf: inputSourceSchema(), BodySchema: body, Output: executionResultSchema(), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		var input valuehandoff.ReportInput
		if e := executionInput(ctx, streams, r, &input); e != nil {
			return nil, e
		}
		s, e := a.executionStore(r)
		if e != nil {
			return nil, e
		}
		return s.Report(ctx, r.Args[0], r.Options["request-id"], input)
	}})
}

func (a *App) registerExecutionDelivery() {
	a.commands = append(a.commands, Command{Name: "values execution deliver", Description: "Initiate at most one approved private send; uncertain attempts never resend.", Options: executionOptions(), Arguments: []Argument{{Name: "execution-id", Required: true, Pattern: valuehandoff.IDPattern}}, InputOneOf: inputSourceSchema(), BodySchema: object(map[string]any{"revision": map[string]any{"type": "integer", "minimum": 1, "maximum": uint64(1<<64 - 1)}}, "revision"), Output: executionResultSchema(), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		var input valuehandoff.RevisionInput
		if e := executionInput(ctx, streams, r, &input); e != nil {
			return nil, e
		}
		s, e := a.executionStore(r)
		if e != nil {
			return nil, e
		}
		return s.Deliver(ctx, r.Args[0], r.Options["request-id"], input)
	}})
}

func (a *App) registerExecutionControls() {
	for _, action := range []string{"approve", "cancel"} {
		fields := map[string]any{"revision": map[string]any{"type": "integer", "minimum": 1, "maximum": uint64(1<<64 - 1)}}
		required := []string{"revision"}
		if action == "approve" {
			fields["runner_id"] = map[string]any{"type": "string", "pattern": valuehandoff.UUIDPattern}
			fields["runner_instance_id"] = map[string]any{"type": "string", "pattern": valuehandoff.UUIDPattern}
			fields["input_id"] = map[string]any{"type": "string", "pattern": valuehandoff.IDPattern}
			fields["external_execution_id"] = map[string]any{"type": "string", "pattern": `^[ -~]+$`, "minLength": 1, "maxLength": 128}
			required = append(required, "runner_id", "runner_instance_id", "external_execution_id", "input_id")
		}
		a.commands = append(a.commands, Command{Name: "values execution " + action, Description: "Explicitly " + action + " a frozen execution input; this does not start or cancel an external job.", Options: executionOptions(), Arguments: []Argument{{Name: "execution-id", Required: true, Pattern: valuehandoff.IDPattern}}, InputOneOf: inputSourceSchema(), BodySchema: object(fields, required...), Output: executionResultSchema(), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			s, e := a.executionStore(r)
			if e != nil {
				return nil, e
			}
			if action == "approve" {
				var input valuehandoff.ApprovalInput
				if e := executionInput(ctx, streams, r, &input); e != nil {
					return nil, e
				}
				return s.Approve(ctx, r.Args[0], r.Options["request-id"], input)
			}
			var input valuehandoff.RevisionInput
			if e := executionInput(ctx, streams, r, &input); e != nil {
				return nil, e
			}
			return s.Cancel(ctx, r.Args[0], r.Options["request-id"], input)
		}})
	}
}
