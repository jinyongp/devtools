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
			if option.Boolean {
				s = map[string]any{"type": "boolean"}
			}
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
		argumentSchemas := []any{}
		minimum := 0
		for _, argument := range command.Arguments {
			s := map[string]any{"type": "string", "minLength": 1, "description": argument.Name}
			if argument.Pattern != "" {
				s["pattern"] = argument.Pattern
			}
			argumentSchemas = append(argumentSchemas, s)
			if argument.Required {
				minimum++
			}
		}
		argsSchema := map[string]any{"type": "array", "minItems": minimum, "maxItems": len(command.Arguments)}
		if len(argumentSchemas) > 0 {
			argsSchema["prefixItems"] = argumentSchemas
		}
		properties["args"] = argsSchema
		if minimum > 0 {
			required = append(required, "args")
		}
		if command.ChildArgs {
			properties["child_args"] = map[string]any{"type": "array", "items": stringSchema()}
		}
		input := object(properties, required...)
		if len(required) == 0 {
			delete(input, "required")
		}
		if command.InputOneOf != nil {
			input["oneOf"] = command.InputOneOf
		}
		if command.ChildArgs {
			input["anyOf"] = []any{
				map[string]any{"required": []string{"args"}, "properties": map[string]any{"args": map[string]any{"minItems": 1}}},
				map[string]any{"required": []string{"child_args"}, "properties": map[string]any{"child_args": map[string]any{"minItems": 1}}},
			}
		}
		commands = append(commands, map[string]any{"name": command.Name, "aliases": command.Aliases, "description": command.Description, "options": command.Options, "arguments": command.Arguments, "accepts_child_args": command.ChildArgs, "stream_output": command.StreamOutput, "input_schema": input, "output_schema": command.Output})
	}
	return map[string]any{
		"protocol_version": protocol.Version,
		"commands":         commands,
		"transport":        map[string]any{"input": "CLI flags and positional args; child_args follow --. Schemas describe parsed inputs, not a JSON stdin endpoint. Secret --stdin reads a raw UTF-8 value.", "success": "one JSON response on stdout", "failure": "one JSON response on stderr", "interactive": false, "help_flags": []string{"--help", "-h"}},
		"exit_codes":       map[string]string{"0": "success", "1": "io_error or storage_error", "2": "invalid_argument", "3": "project_not_found, invalid_config, profile_conflict, env_not_found, env_not_empty, key_not_found, kind_conflict, invalid_storage", "130": "canceled"},
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
