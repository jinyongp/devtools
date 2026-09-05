package cli

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/protocol"
)

func argumentError(message, field string) *protocol.Error {
	var details map[string]any
	if field != "" {
		details = map[string]any{"field": field}
	}
	return protocol.NewError("invalid_argument", message, 2, details)
}

func parseRequest(command Command, tokens []string) (Request, *protocol.Error) {
	request := Request{Options: map[string]string{}, ListOptions: map[string][]string{}, Args: []string{}}
	definitions := map[string]Option{}
	for _, option := range command.Options {
		definitions[option.Name] = option
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token == "--" {
			if !command.ChildArgs {
				return request, argumentError("This command accepts only declared options and arguments.", "")
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
			continue
		}
		if !strings.HasPrefix(token, "--") {
			return request, argumentError("Unknown option. Run devtools schema for accepted inputs.", "")
		}
		name, value, hasValue := strings.Cut(token[2:], "=")
		option, exists := definitions[name]
		if !exists {
			return request, argumentError("Unknown option. Run devtools schema for accepted inputs.", "")
		}
		if _, exists := request.Options[name]; exists && !option.Repeatable {
			return request, argumentError("Specify each option once.", name)
		}
		if option.Boolean {
			if !hasValue {
				value = "true"
			}
			if value != "true" && value != "false" {
				return request, argumentError("Expected a boolean option.", name)
			}
		} else if !hasValue {
			i++
			if i >= len(tokens) || tokens[i] == "--" {
				return request, argumentError("Option requires a value.", name)
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
				return request, argumentError("Required option is missing.", option.Name)
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
				return request, argumentError("Option does not match its schema.", option.Name)
			}
		}
	}
	if len(request.Args) > len(command.Arguments) {
		return request, argumentError("Unexpected positional arguments.", "")
	}
	for i, argument := range command.Arguments {
		if i >= len(request.Args) {
			if argument.Required {
				return request, argumentError("Required argument is missing.", argument.Name)
			}
			continue
		}
		if request.Args[i] == "" || (argument.Pattern != "" && !regexp.MustCompile(argument.Pattern).MatchString(request.Args[i])) {
			return request, argumentError("Argument does not match its schema.", argument.Name)
		}
	}
	return request, nil
}
