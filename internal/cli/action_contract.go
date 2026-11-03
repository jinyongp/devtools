package cli

import (
	"strings"
	"sync"

	"github.com/jinyongp/devtools/internal/protocol"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// ResolveAction uses the live command registry and its parser/static predicates.
// It deliberately performs no handler, input, filesystem or session operation.
func (a *App) ResolveAction(argv []string) *protocol.Error {
	if len(argv) < 2 || argv[0] != "devtools" {
		return argumentError("Use a complete devtools command.", "command")
	}
	root, e := parseRootOptions(argv[1:])
	if e != nil {
		return e
	}
	args := root.CommandArgs
	if len(args) == 0 {
		return argumentError("Select a command.", "command")
	}
	if args[0] == "help" || args[0] == "schema" {
		if len(args) == 2 && args[1] == "--all" && args[0] == "schema" {
			return nil
		}
		if _, _, ok := a.discovery(strings.Join(args[1:], " "), args[0] == "schema"); ok {
			return nil
		}
		return argumentError("Select an existing help or schema target.", "command")
	}
	var selected *Command
	var rest []string
	for i := range a.commands {
		for _, name := range append([]string{a.commands[i].Name}, a.commands[i].Aliases...) {
			words := strings.Fields(name)
			if len(args) >= len(words) && strings.Join(args[:len(words)], " ") == name && (selected == nil || len(words) > len(args)-len(rest)) {
				selected, rest = &a.commands[i], args[len(words):]
			}
		}
	}
	if selected == nil {
		return argumentError("Select a registered command.", "command")
	}
	r, e := parseRequest(*selected, rest)
	if e != nil {
		return e
	}
	if root.IsolatedRoot != "" && r.Options["isolated-root"] != "" {
		return argumentError("Specify the isolation root once.", "isolated-root")
	}
	for _, rule := range selected.StaticRules {
		if e := rule.failure(r); e != nil {
			return e
		}
	}
	return a.validateActionInput(*selected, r)
}

// The parser owns tokens, primitive constraints and defaults. These are the
// same declared cross-field constraints published in input_schema, including
// body-source alternatives that cannot be inferred from option patterns.
func (a *App) validateActionInput(c Command, r Request) *protocol.Error {
	if c.InputOneOf == nil && c.InputAllOf == nil {
		return nil
	}
	cache := a.actionContracts
	if cache == nil {
		cache = &sync.Map{}
	}
	compiled, ok := cache.Load(c.Name)
	if !ok {
		schema := map[string]any{"type": "object"}
		if c.InputOneOf != nil {
			schema["oneOf"] = c.InputOneOf
		}
		if c.InputAllOf != nil {
			schema["allOf"] = c.InputAllOf
		}
		compiler := jsonschema.NewCompiler()
		if e := compiler.AddResource("urn:devtools:action", publicResultObject(schema)); e != nil {
			return argumentError("Cannot validate this command contract.", "command")
		}
		contract, e := compiler.Compile("urn:devtools:action")
		if e != nil {
			return argumentError("Cannot validate this command contract.", "command")
		}
		compiled, _ = cache.LoadOrStore(c.Name, contract)
	}
	input := map[string]any{}
	toArray := func(values []string) []any {
		out := []any{}
		for _, v := range values {
			out = append(out, v)
		}
		return out
	}
	for _, o := range c.Options {
		v, supplied := r.Options[o.Name]
		if r.OptionOffsets != nil {
			_, supplied = r.OptionOffsets[o.Name]
		}
		if !supplied {
			continue
		}
		if o.Repeatable {
			input[o.Name] = toArray(r.ListOptions[o.Name])
		} else if o.Boolean {
			input[o.Name] = v == "true"
		} else {
			input[o.Name] = v
		}
	}
	input["args"] = toArray(r.Args)
	if c.ChildArgs {
		input["child_args"] = toArray(r.Child)
	}
	if compiled.(*jsonschema.Schema).Validate(input) != nil {
		return argumentError("Supply the command's required inputs and choose compatible input methods; inspect its help or schema.", "command")
	}
	return nil
}
