package cli

import "github.com/jinyongp/devtools/internal/protocol"

func (a *App) catalog() map[string]any {
	commands := make([]map[string]any, 0, len(a.commands))
	for _, command := range a.commands {
		properties := map[string]any{}
		required := []string{}
		for _, option := range command.Options {
			if option.Required {
				required = append(required, option.Name)
			}
			s := stringSchema()
			s["description"] = option.Description
			if option.Default != "" {
				s["default"] = option.Default
			}
			if option.Pattern != "" {
				s["pattern"] = option.Pattern
			}
			if option.MinLength != 0 {
				s["minLength"] = option.MinLength
			}
			if option.MaxLength != 0 {
				s["maxLength"] = option.MaxLength
			}
			properties[option.Name] = s
		}
		input := object(properties, required...)
		if len(required) == 0 {
			delete(input, "required")
		}
		commands = append(commands, map[string]any{"name": command.Name, "description": command.Description, "options": command.Options, "input_schema": input, "output_schema": command.Output})
	}
	return map[string]any{
		"protocol_version": protocol.Version,
		"commands":         commands,
		"transport":        map[string]any{"input": "CLI flags; input schemas describe flag names and values, not a JSON stdin endpoint", "success": "one JSON response on stdout", "failure": "one JSON response on stderr", "interactive": false, "help_flags": []string{"--help", "-h"}},
		"exit_codes":       map[string]string{"0": "success", "1": "io_error or internal failure", "2": "invalid_argument", "3": "project_not_found or invalid_config", "130": "canceled"},
		"response_schema": map[string]any{
			"$schema":              "https://json-schema.org/draft/2020-12/schema",
			"type":                 "object",
			"properties":           map[string]any{"schema_version": map[string]any{"const": protocol.Version}, "ok": map[string]any{"type": "boolean"}, "data": map[string]any{}, "error": object(map[string]any{"code": stringSchema(), "message": stringSchema(), "details": map[string]any{"type": "object"}}, "code", "message")},
			"required":             []string{"schema_version", "ok"},
			"additionalProperties": false,
			"oneOf": []any{
				map[string]any{"properties": map[string]any{"ok": map[string]any{"const": true}}, "required": []string{"data"}, "not": map[string]any{"required": []string{"error"}}},
				map[string]any{"properties": map[string]any{"ok": map[string]any{"const": false}}, "required": []string{"error"}, "not": map[string]any{"required": []string{"data"}}},
			},
		},
		"$defs": map[string]any{"catalog": map[string]any{"type": "object", "required": []string{"protocol_version", "commands", "transport", "exit_codes", "response_schema", "$defs"}, "properties": map[string]any{"protocol_version": map[string]any{"const": protocol.Version}, "commands": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "transport": map[string]any{"type": "object"}, "exit_codes": map[string]any{"type": "object"}, "response_schema": map[string]any{"type": "object"}, "$defs": map[string]any{"type": "object"}}, "additionalProperties": false}},
	}
}
