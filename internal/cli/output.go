package cli

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

var previewCommands = map[string]bool{
	"config fmt": true, "config edit": true, "config migrate": true,
	"config tool add": true, "config tool update": true, "config tool remove": true,
	"config preset add": true, "config preset update": true, "config preset remove": true,
	"command add": true, "command update": true, "command remove": true, "command rename": true,
	"bundle set": true, "bundle rename": true, "bundle remove": true,
	"profile import": true, "backup restore": true, "proxy teardown": true,
}

// Invocation presentation metadata belongs at the public boundary, never in
// value receipts, journals, replay fingerprints or persisted cursor identities.
func previewResultContract(c *Command) {
	if c.Name == "values execution list" {
		c.Output["properties"].(map[string]any)["next_actions"] = arraySchema(remedySchema())
		run := c.Run
		c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			value, failure := run(ctx, streams, r)
			if failure != nil {
				return value, failure
			}
			out := publicResultObject(value)
			if out == nil {
				return value, nil
			}
			if cursor, ok := out["next_cursor"].(string); ok && cursor != "" {
				argv := []string{"devtools", "values", "execution", "list", "--profile", out["profile"].(string)}
				for _, key := range []string{"limit", "query-cache-dir"} {
					if v := r.Options[key]; v != "" {
						argv = append(argv, "--"+key, v)
					}
				}
				argv = append(argv, "--cursor", cursor)
				out["next_actions"] = []protocol.Remedy{protocol.LocalAction(argv, nil, "Read the next page.", "read").Remedy(r.Invocation)}
			}
			return out, nil
		}
	}
	if !previewCommands[c.Name] {
		return
	}
	publicResultField(c.Output, "dry_run", map[string]any{"type": "boolean"}, true)
	publicResultField(c.Output, "next_actions", arraySchema(remedySchema()), false)
	run := c.Run
	c.Run = func(ctx context.Context, streams IO, request Request) (any, *protocol.Error) {
		result, failure := run(ctx, streams, request)
		dry := request.Options["dry-run"] == "true" || c.Name == "config fmt" && request.Options["check"] == "true" || c.Name == "config migrate" && request.Options["apply"] == ""
		if c.Name == "backup restore" || c.Name == "profile import" || c.Name == "proxy teardown" {
			dry = request.Options["apply"] == ""
		}
		project := func(value any) any {
			out := publicResultObject(value)
			if out == nil {
				return value
			}
			out["dry_run"] = dry
			if dry {
				out["next_actions"] = []protocol.Remedy{previewApplyAction(*c, request, out)}
			}
			return out
		}
		if failure != nil {
			if report := failure.Details["report"]; report != nil {
				projected := project(report)
				failure.Details["report"] = projected
				if c.Name == "config fmt" {
					if value, ok := projected.(map[string]any); ok {
						failure.Details["remedies"] = value["next_actions"]
					}
				}
			}
			return result, failure
		}
		return project(result), nil
	}
}

func publicResultObject(value any) map[string]any {
	b, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if d.Decode(&out) != nil {
		return nil
	}
	return out
}

// OutputMode describes who owns stdout. Only JSON data is envelope-encoded.
type OutputMode string

const (
	OutputJSON        OutputMode = "json"
	OutputText        OutputMode = "text"
	OutputArtifact    OutputMode = "artifact"
	OutputPassthrough OutputMode = "passthrough"
)

// itemOutput describes a single resource, distinct from query metadata.
func itemOutput(item map[string]any) map[string]any {
	return object(map[string]any{"item": item}, "item")
}

func changedItemOutput(item map[string]any) map[string]any {
	return object(map[string]any{"item": item, "changed": map[string]any{"type": "boolean"}}, "item", "changed")
}

func remedySchema() map[string]any {
	return object(map[string]any{
		"effect":          map[string]any{"enum": []string{"read", "config_write", "value_write", "system_write"}},
		"argv":            map[string]any{"type": "array", "items": stringSchema()},
		"required_inputs": map[string]any{"type": "array", "items": stringSchema()},
		"message":         stringSchema(),
	}, "argv", "required_inputs", "message", "effect")
}
