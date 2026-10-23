package cli

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func argumentError(message, field string) *protocol.Error {
	details := map[string]any{"phase": "parse", "reason": "invalid_value"}
	if field != "" {
		details["field"] = field
		details["facts"] = map[string]any{"field": field}
	}
	return protocol.NewError("invalid_argument", message, 2, details)
}

func inputRule(pattern string, minimum, maximum, actual int) (string, map[string]any) {
	facts := map[string]any{"actual_length": actual}
	if pattern != "" {
		facts["rule"] = pattern
	}
	if minimum > 0 {
		facts["minimum"] = minimum
	}
	if maximum > 0 {
		facts["maximum"] = maximum
	}
	switch pattern {
	case uuidPattern:
		return "Use a UUID with hexadecimal groups of 8-4-4-4-12 characters.", facts
	case project.ProfilePattern:
		return "Use 1–128 ASCII letters, digits, dots, underscores or hyphens, beginning with a letter or digit.", facts
	case positiveIntegerPattern:
		return "Use a positive decimal integer.", facts
	case `^[0-9a-f]{64}$`:
		return "Use the 64-character lowercase hexadecimal revision or digest returned by the corresponding inspection.", facts
	}
	if strings.HasPrefix(pattern, "^(") && strings.HasSuffix(pattern, ")$") {
		choices := strings.Split(strings.TrimSuffix(strings.TrimPrefix(pattern, "^("), ")$"), "|")
		plain := true
		for _, choice := range choices {
			if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(choice) {
				plain = false
			}
		}
		if plain {
			facts["allowed"] = choices
			return "Choose one of: " + strings.Join(choices, ", ") + ".", facts
		}
	}
	return "Inspect this field's pattern and length constraints in the command schema.", facts
}

func parseRequest(command Command, tokens []string) (Request, *protocol.Error) {
	request := Request{Options: map[string]string{}, ListOptions: map[string][]string{}, Args: []string{}, OptionOffsets: map[string]int{}}
	definitions := map[string]Option{}
	argumentOffsets := []int{}
	for _, option := range command.Options {
		definitions[option.Name] = option
	}
	invalid := func(reason, message, field string, index int, facts map[string]any) *protocol.Error {
		if facts == nil {
			facts = map[string]any{}
		}
		if field != "" {
			facts["field"] = field
		}
		return protocol.NewError("invalid_argument", message, 2, map[string]any{"field": field, "phase": "parse", "reason": reason, "facts": facts, "sources": []any{map[string]any{"kind": "argv", "path": []string{field}, "offset": index}}, "remedies": []protocol.Remedy{{Argv: append([]string{"devtools", "help"}, strings.Fields(command.Name)...), RequiredInputs: []string{}, Message: "Inspect this command's accepted inputs and examples.", Effect: "read"}}})
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token == "--" {
			if !command.ChildArgs {
				return request, invalid("conflicting_inputs", "This command does not accept child arguments after --.", "child_args", i, nil)
			}
			request.Child = tokens[i+1:]
			break
		}
		if token == "--help" || token == "-h" {
			request.Help = true
			continue
		}
		if !strings.HasPrefix(token, "-") {
			request.Args = append(request.Args, token)
			argumentOffsets = append(argumentOffsets, i)
			if command.Name == "command run" {
				request.Child = tokens[i+1:]
				if len(request.Child) > 0 && request.Child[0] == "--" {
					request.Child = request.Child[1:]
				}
				break
			}
			continue
		}
		if !strings.HasPrefix(token, "--") {
			return request, invalid("unknown_option", "Unknown option; use this command's help.", token, i, nil)
		}
		name, value, hasValue := strings.Cut(token[2:], "=")
		request.OptionOffsets[name] = i
		option, exists := definitions[name]
		if !exists {
			candidates := []string{}
			for key := range definitions {
				candidates = append(candidates, key)
			}
			return request, invalid("unknown_option", "Unknown option --"+name+"; use this command's help.", name, i, map[string]any{"suggestions": suggestions(name, candidates)})
		}
		if _, exists := request.Options[name]; exists && !option.Repeatable {
			return request, invalid("duplicate_field", "Specify --"+name+" once.", name, i, nil)
		}
		if option.Boolean {
			if !hasValue {
				value = "true"
			}
			if value != "true" && value != "false" {
				return request, invalid("invalid_type", "--"+name+" expects true or false.", name, i, map[string]any{"allowed": []string{"true", "false"}})
			}
		} else if !hasValue {
			i++
			if i >= len(tokens) || strings.HasPrefix(tokens[i], "--") {
				return request, invalid("missing_option_value", "Option --"+name+" requires a value; use --"+name+"=VALUE for a value beginning with --.", name, i-1, nil)
			}
			value = tokens[i]
		}
		request.Options[name] = value
		if option.Repeatable {
			request.ListOptions[name] = append(request.ListOptions[name], value)
		}
	}
	if request.Help {
		return request, nil
	}
	for _, option := range command.Options {
		value, exists := request.Options[option.Name]
		if !exists {
			if option.Required {
				return request, invalid("missing_argument", "Required option --"+option.Name+" is missing.", option.Name, len(tokens), nil)
			}
			if option.Default == "" {
				continue
			}
			value = option.Default
			request.Options[option.Name] = value
		}
		values := []string{value}
		if option.Repeatable {
			values = request.ListOptions[option.Name]
		}
		for _, value := range values {
			length := utf8.RuneCountInString(value)
			if length < option.MinLength || (option.MaxLength > 0 && length > option.MaxLength) || (option.Pattern != "" && !regexp.MustCompile(option.Pattern).MatchString(value)) {
				rule, facts := inputRule(option.Pattern, option.MinLength, option.MaxLength, length)
				return request, invalid("invalid_value", "Invalid option --"+option.Name+". "+rule, option.Name, request.OptionOffsets[option.Name], facts)
			}
		}
	}
	repeatable := -1
	for i, argument := range command.Arguments {
		if !argument.Repeatable {
			continue
		}
		if repeatable != -1 || i != len(command.Arguments)-1 {
			return request, protocol.NewError("internal_error", "Command definition has an invalid repeatable argument.", 1, nil)
		}
		repeatable = i
	}
	if repeatable == -1 && len(request.Args) > len(command.Arguments) {
		return request, invalid("conflicting_inputs", "Unexpected positional arguments; inspect this command's usage.", "args", len(tokens)-1, nil)
	}
	validArgument := func(argument Argument, value string) bool {
		return value != "" && (argument.Pattern == "" || regexp.MustCompile(argument.Pattern).MatchString(value))
	}
	if command.UniqueArgs {
		seen := map[string]bool{}
		for _, value := range request.Args {
			if seen[value] {
				return request, invalid("duplicate_field", "Specify each positional argument once.", "args", len(tokens)-1, nil)
			}
			seen[value] = true
		}
	}
	for i, argument := range command.Arguments {
		if i >= len(request.Args) {
			if argument.Required {
				return request, invalid("missing_argument", "Required argument "+argument.Name+" is missing.", argument.Name, len(tokens), nil)
			}
			continue
		}
		if argument.Repeatable {
			for j, value := range request.Args[i:] {
				if !validArgument(argument, value) {
					return request, invalid("invalid_value", "Argument "+argument.Name+" does not match its schema.", argument.Name, argumentOffsets[i+j], map[string]any{"rule": argument.Pattern})
				}
			}
			break
		}
		if !validArgument(argument, request.Args[i]) {
			return request, invalid("invalid_value", "Argument "+argument.Name+" does not match its schema.", argument.Name, argumentOffsets[i], map[string]any{"rule": argument.Pattern})
		}
	}
	if command.Name == "values execution status" && (len(request.Args) == 0) == (request.Options["request-id"] == "") {
		return request, invalid("conflicting_inputs", "Provide exactly one execution ID or --request-id from the original prepare operation.", "execution-id", len(tokens), nil)
	}
	return request, nil
}
