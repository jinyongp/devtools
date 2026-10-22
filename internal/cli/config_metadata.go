package cli

func trueFlag(name string) map[string]any {
	return map[string]any{"required": []string{name}, "properties": map[string]any{name: map[string]any{"const": true}}}
}
func configMetadata(c *Command) {
	if c.BodySchema != nil {
		file := map[string]any{"required": []string{"file"}, "not": trueFlag("stdin")}
		stdin := trueFlag("stdin")
		stdin["not"] = map[string]any{"required": []string{"file"}}
		c.InputOneOf = []map[string]any{file, stdin}
		if c.Name == "command add" {
			child := map[string]any{"required": []string{"child_args"}, "properties": map[string]any{"child_args": map[string]any{"minItems": 1}}}
			file["not"] = map[string]any{"anyOf": []any{trueFlag("stdin"), child}}
			stdin["not"] = map[string]any{"anyOf": []any{map[string]any{"required": []string{"file"}}, child}}
			child["not"] = map[string]any{"anyOf": []any{trueFlag("stdin"), map[string]any{"required": []string{"file"}}}}
			c.InputOneOf = append(c.InputOneOf, child)
		}
	}
	if c.Name == "config fmt" {
		c.InputAllOf = []map[string]any{{"not": map[string]any{"allOf": []any{trueFlag("check"), trueFlag("dry-run")}}}}
	}
	if c.Name == "config migrate" {
		c.InputAllOf = []map[string]any{{"if": map[string]any{"required": []string{"apply"}}, "then": map[string]any{"required": []string{"revision"}, "not": trueFlag("dry-run")}}}
	}
}
