package cli

import (
	"encoding/json"
	"sort"
	"strings"
)

func exampleValue(name, pattern string) string {
	if pattern == "^(native|passphrase)$" {
		return "passphrase"
	}
	if pattern == positiveIntegerPattern {
		return "1"
	}
	if strings.Contains(pattern, "[0-9a-f]{64}") {
		return strings.Repeat("0", 64)
	}
	if strings.Contains(pattern, "[0-9a-f]{32}") {
		return strings.Repeat("0", 32)
	}
	if strings.Contains(pattern, "{8}-") {
		return "00000000-0000-4000-8000-000000000000"
	}
	if name == "profile" {
		return "sample"
	}
	if name == "shell" {
		return "bash"
	}
	if name == "revision" {
		return strings.Repeat("0", 64)
	}
	if name == "port" {
		return "3000"
	}
	return "sample"
}
func commandExamples(c Command) []string {
	if c.Name == "backup keygen" {
		return []string{"devtools backup keygen --identity-file ./backup.identity --recipient-file ./backup.recipient"}
	}
	if c.Name == "proxy setup" {
		return []string{"devtools proxy setup --check"}
	}
	if c.Name == "proxy run" {
		return []string{"devtools proxy run --global sample -- PROGRAM"}
	}
	line := "devtools " + c.Name
	for _, arg := range c.Arguments {
		if arg.Required {
			line += " " + exampleValue(arg.Name, arg.Pattern)
		}
	}
	for _, o := range c.Options {
		if o.Required {
			line += " --" + o.Name
			if !o.Boolean {
				line += " " + exampleValue(o.Name, o.Pattern)
			}
		}
	}
	if c.BodySchema != nil {
		line += " --stdin"
	}
	if c.Name == "secret set" {
		line += " --stdin"
	}
	if c.Name == "variable set" {
		line += " --value sample"
	}
	if c.Name == "task workstream edit" {
		line += " --dry-run"
	}
	if c.Name == "command run" {
		line = "devtools run dev:web"
	}
	if c.Name == "command add" {
		line = "devtools command add dev:web -- node scripts/dev.mjs"
	}
	if c.Name == "values execution status" {
		line += " " + strings.Repeat("0", 32)
	}
	switch c.Name {
	case "proxy add", "proxy rm":
		line += " --global"
	case "proxy clear":
		line += " --global --all"
	case "proxy up", "proxy down":
		line += " --all"
	}
	return []string{line}
}

func commandWorkflow(c Command) []string {
	switch c.Name {
	case "vault rewrap":
		return []string{
			"Discover schema vault rewrap and inspect vault status. Select the actual original and target backend explicitly; this changes protection of the same root key, not stored values or provider credentials.",
			"Replace the syntax example's UUID once for a new change. Provide original and target passphrases through hidden terminal input or private files; only one side may read piped stdin. Native authentication UI requires --interactive.",
			"After interruption retain the original UUID, backend selections and target credentials. Inspect vault status; after slot publication the retired original credentials are no longer required. Follow pending-operation remedies rather than reinitializing.",
			"Rewrap preserves manual lock. Explicitly unlock the target protection before using a locked passphrase session. Existing app environments, provider credentials and old copies are not revoked.",
		}
	case "secret generate":
		return []string{
			"Read devtools values revision --profile sample for the current edit revision; inspect existing key metadata before selecting --replace.",
			"Replace the syntax example's sample profile/key and zero revision with those actual selections. Generate one fresh UUID for this new operation.",
			"Run the edited command. If its response is lost, keep the original UUID and identical input; inspect devtools values request show REQUEST_UUID --profile sample before retrying.",
			"Inspect devtools secret list --profile sample for metadata; secret plaintext is never returned.",
		}
	case "values execution status":
		return []string{
			"Obtain the execution ID from the prepare response or devtools values execution list --profile sample.",
			"Replace the zero ID in the syntax example, or omit it and use --request-id with the original prepare UUID. Supply exactly one selector.",
			"An unknown delivery requires receiver/execution inspection; do not prepare or deliver new work merely to recover a response.",
		}
	}
	for _, o := range c.Options {
		if o.Name == "request-id" && o.Required {
			return []string{"The example is syntax only. Obtain required resource IDs, revisions and preview digests from the operation's inspection workflow.", "Generate a new UUID once for a new operation. After an uncertain response, retain that UUID and identical input and inspect saved state before retrying."}
		}
	}
	return nil
}
func bodyExample(c Command) any {
	// Schemas are assembled from both typed Go slices and decoded JSON. Normalize
	// them once so example generation follows the same representation as consumers.
	b, _ := json.Marshal(c.BodySchema)
	var s map[string]any
	_ = json.Unmarshal(b, &s)
	defs, _ := s["$defs"].(map[string]any)
	// Published body schemas are self-contained. Configuration commands include
	// their referenced definitions; task/value bodies need no external schema.
	var sample func(map[string]any, int) any
	sample = func(schema map[string]any, depth int) any {
		if depth > 12 {
			return nil
		}
		if ref, ok := schema["$ref"].(string); ok {
			if target, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any); ok {
				return sample(target, depth+1)
			}
		}
		if v, ok := schema["const"]; ok {
			return v
		}
		if v, ok := schema["default"]; ok {
			return v
		}
		if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
			return values[0]
		}
		for _, key := range []string{"oneOf", "anyOf"} {
			if list, ok := schema[key].([]any); ok && len(list) > 0 {
				merged := map[string]any{}
				for k, v := range schema {
					if k != key {
						merged[k] = v
					}
				}
				for k, v := range list[0].(map[string]any) {
					if k == "required" {
						existing, _ := merged[k].([]any)
						v = append(append([]any{}, existing...), v.([]any)...)
					}
					if k == "properties" {
						properties := map[string]any{}
						existing, _ := merged[k].(map[string]any)
						for name, field := range existing {
							properties[name] = field
						}
						for name, field := range v.(map[string]any) {
							properties[name] = field
						}
						v = properties
					}
					merged[k] = v
				}
				return sample(merged, depth+1)
			}
		}
		typ := schema["type"]
		if types, ok := typ.([]any); ok && len(types) > 0 {
			typ = types[0]
		}
		switch typ {
		case "object":
			out := map[string]any{}
			properties, _ := schema["properties"].(map[string]any)
			required, _ := schema["required"].([]any)
			for _, key := range required {
				if field, ok := properties[key.(string)].(map[string]any); ok {
					out[key.(string)] = sample(field, depth+1)
				}
			}
			minimum, _ := schema["minProperties"].(float64)
			keys := make([]string, 0, len(properties))
			for key := range properties {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if len(out) >= int(minimum) {
					break
				}
				if _, exists := out[key]; !exists {
					out[key] = sample(properties[key].(map[string]any), depth+1)
				}
			}
			return out
		case "array":
			out := []any{}
			count, _ := schema["minItems"].(float64)
			item, _ := schema["items"].(map[string]any)
			for i := 0; i < int(count); i++ {
				out = append(out, sample(item, depth+1))
			}
			return out
		case "boolean":
			return false
		case "integer", "number":
			if v, ok := schema["minimum"]; ok {
				return v
			}
			return 1
		case "null":
			return nil
		default:
			if schema["format"] == "uuid" {
				return "00000000-0000-4000-8000-000000000000"
			}
			if pattern, ok := schema["pattern"].(string); ok {
				return exampleValue("", pattern)
			}
			return "sample"
		}
	}
	if c.Name == "config edit" {
		return map[string]any{"operations": []any{map[string]any{"op": "tool.add", "name": "node", "value": map[string]any{}}}}
	}
	switch c.Name {
	case "command add":
		return map[string]any{"exec": []any{"node", "scripts/dev.mjs"}}
	case "config tool update":
		return map[string]any{"version": "1.2.3"}
	case "config preset update":
		return map[string]any{"tools": []any{}}
	case "command update":
		return map[string]any{"inject": true}
	}
	return sample(s, 0)
}
