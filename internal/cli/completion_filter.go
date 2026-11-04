package cli

import "reflect"

// Partial input can still gain options and arguments. Evaluate only definite
// contradictions in the same input schemas used by parsing and discovery.
type completionTruth uint8

const (
	completionFalse completionTruth = 1 << iota
	completionTrue
	completionUnknown = completionFalse | completionTrue
)

type completionUnknownValue struct{}

func completionNot(value completionTruth) completionTruth {
	return value<<1&completionTrue | value>>1&completionFalse
}

func completionAll(values []completionTruth) completionTruth {
	out := completionTrue
	for _, value := range values {
		if value&completionFalse != 0 {
			out |= completionFalse
		}
		if value&completionTrue == 0 {
			out &^= completionTrue
		}
	}
	return out
}

func completionSchema(schema map[string]any, input any, possible map[string]bool) completionTruth {
	if _, unknown := input.(completionUnknownValue); unknown {
		return completionUnknown
	}
	checks := []completionTruth{}
	known := func(ok bool) completionTruth {
		if ok {
			return completionTrue
		}
		return completionFalse
	}
	if value, exists := schema["const"]; exists {
		checks = append(checks, known(reflect.DeepEqual(input, value)))
	}
	if values, exists := schema["enum"]; exists {
		found := false
		v := reflect.ValueOf(values)
		for i := 0; i < v.Len(); i++ {
			found = found || reflect.DeepEqual(v.Index(i).Interface(), input)
		}
		checks = append(checks, known(found))
	}
	if minimum, exists := schema["minItems"].(int); exists {
		v := reflect.ValueOf(input)
		checks = append(checks, known(v.IsValid() && v.Kind() == reflect.Slice && v.Len() >= minimum))
	}
	if object, ok := input.(map[string]any); ok {
		required := reflect.ValueOf(schema["required"])
		if required.IsValid() {
			for i := 0; i < required.Len(); i++ {
				name := required.Index(i).Interface().(string)
				if _, exists := object[name]; exists {
					checks = append(checks, completionTrue)
				} else if possible[name] {
					checks = append(checks, completionUnknown)
				} else {
					checks = append(checks, completionFalse)
				}
			}
		}
		if properties, ok := schema["properties"].(map[string]any); ok {
			for name, property := range properties {
				if value, exists := object[name]; exists {
					checks = append(checks, completionSchema(property.(map[string]any), value, nil))
				} else if possible[name] {
					checks = append(checks, completionUnknown)
				}
			}
		}
	}
	if child, ok := schema["not"].(map[string]any); ok {
		checks = append(checks, completionNot(completionSchema(child, input, possible)))
	}
	for _, name := range []string{"allOf", "anyOf", "oneOf"} {
		branches := reflect.ValueOf(schema[name])
		if !branches.IsValid() {
			continue
		}
		values := []completionTruth{}
		minimum, maximum := 0, 0
		for i := 0; i < branches.Len(); i++ {
			value := completionSchema(branches.Index(i).Interface().(map[string]any), input, possible)
			values = append(values, value)
			if value == completionTrue {
				minimum++
			}
			if value&completionTrue != 0 {
				maximum++
			}
		}
		value := completionUnknown
		switch name {
		case "allOf":
			value = completionAll(values)
		case "anyOf":
			if minimum > 0 {
				value = completionTrue
			} else if maximum == 0 {
				value = completionFalse
			}
		case "oneOf":
			if minimum > 1 || maximum == 0 {
				value = completionFalse
			} else if minimum == 1 && maximum == 1 {
				value = completionTrue
			}
		}
		checks = append(checks, value)
	}
	if condition, ok := schema["if"].(map[string]any); ok {
		value := completionSchema(condition, input, possible)
		result := completionTruth(0)
		for _, branch := range []struct {
			when completionTruth
			name string
		}{{completionTrue, "then"}, {completionFalse, "else"}} {
			if value&branch.when != 0 {
				if child, ok := schema[branch.name].(map[string]any); ok {
					result |= completionSchema(child, input, possible)
				} else {
					result |= completionTrue
				}
			}
		}
		checks = append(checks, result)
	}
	return completionAll(checks)
}

func completionInputPossible(command Command, options map[string]string, arguments []string, unknown string) bool {
	input, possible := map[string]any{}, map[string]bool{}
	for _, option := range command.Options {
		possible[option.Name] = true
		if value, exists := options[option.Name]; exists {
			if option.Name == unknown {
				input[option.Name] = completionUnknownValue{}
			} else if option.Boolean {
				input[option.Name] = value == "true"
			} else {
				input[option.Name] = value
			}
		}
	}
	possible["args"], possible["child_args"] = len(command.Arguments) > 0, command.ChildArgs
	if len(arguments) > 0 || !possible["args"] {
		input["args"] = arguments
	}
	schema := map[string]any{}
	if command.InputOneOf != nil {
		schema["oneOf"] = command.InputOneOf
	}
	if command.InputAllOf != nil {
		schema["allOf"] = command.InputAllOf
	}
	return completionSchema(schema, input, possible)&completionTrue != 0
}
