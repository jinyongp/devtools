package protocol

import (
	"encoding/json"
	"strings"
)

func decodeActionMap(m map[string]any, r *Remedy) bool {
	b, err := json.Marshal(m)
	return err == nil && json.Unmarshal(b, r) == nil
}

func prepareAction(r Remedy, scope InvocationContext, resolve ActionResolver) Remedy {
	bound := (Action{Argv: r.Argv, RequiredInputs: r.RequiredInputs, Message: r.Message, Effect: r.Effect, Relation: r.Relation}).Remedy(scope)
	r.Argv, r.RequiredInputs = bound.Argv, bound.RequiredInputs
	for i := range r.InputSteps {
		r.InputSteps[i] = prepareAction(r.InputSteps[i], scope, resolve)
	}
	// Legacy new-request placeholders are upgraded at this public preparation
	// boundary. Original real request UUIDs are never replaced.
	for i, word := range r.Argv {
		if i == 0 || word != "REQUEST_UUID" || r.Argv[i-1] != "--request-id" {
			continue
		}
		newRequest := false
		for _, input := range r.RequiredInputs {
			x := strings.ToLower(input)
			if strings.Contains(x, "new") && strings.Contains(x, "uuid") {
				newRequest = true
			}
		}
		if newRequest {
			r.Argv[i] = NewRequestID()
			remaining := []string{}
			for _, input := range r.RequiredInputs {
				x := strings.ToLower(input)
				if !strings.Contains(x, "new") || !strings.Contains(x, "uuid") {
					remaining = append(remaining, input)
				}
			}
			r.RequiredInputs = remaining
		}
	}
	if len(r.RequiredInputs) > 0 || r.State == "needs_input" {
		r.State = "needs_input"
		for name, value := range ActionBindings(r.Argv) {
			if r.Bindings == nil {
				r.Bindings = map[string]string{}
			}
			r.Bindings[name] = value
		}
		if len(r.Inputs) == 0 {
			for _, name := range r.RequiredInputs {
				source, methods := "selection", []string{"explicit_option"}
				lower := strings.ToLower(name)
				if strings.Contains(lower, "private") || strings.Contains(lower, "stdin") || strings.Contains(lower, "passphrase") || strings.Contains(lower, "credential") {
					source, methods = "private", []string{"private_file", "stdin"}
				}
				r.Inputs = append(r.Inputs, ActionInput{Name: name, Source: source, Methods: methods, Description: "Supply this input explicitly, then preview the operation again."})
			}
		}
		if len(r.InputSteps) == 0 {
			r.InputSteps = inputHelp(r.Argv, resolve)
		}
		r.Argv = []string{}
		return r
	}
	if len(r.Argv) == 0 || r.State == "manual" || r.Relation == ManualAction || r.Argv[0] != "devtools" {
		r.State = "manual"
		return r
	}
	if resolve != nil {
		if e := resolve(r.Argv); e != nil {
			r.State = "needs_input"
			r.RequiredInputs = []string{"valid command inputs"}
			r.Inputs = []ActionInput{{Name: "command", Source: "contract", Methods: []string{"help"}, Description: e.Message}}
			r.InputSteps = inputHelp(r.Argv, resolve)
			r.Argv = []string{}
			return r
		}
	}
	r.State = "ready"
	return r
}

// ActionBindings preserves reviewed public selectors when private input must be
// supplied again. File paths, values and child arguments are never retained.
func ActionBindings(argv []string) map[string]string {
	var bindings map[string]string
	for i, word := range argv {
		if word == "--" {
			break
		}
		name, value, inline := strings.Cut(word, "=")
		switch name {
		case "--isolated-root", "--profile", "--env", "--bundle", "--request-id", "--if-revision", "--revision", "--at-revision", "--expected-run", "--workstream", "--apply", "--preview-digest", "--protection", "--from-backend", "--backend", "--kind", "--dir":
			if !inline && i+1 < len(argv) {
				value = argv[i+1]
			}
			if inline || i+1 < len(argv) {
				if bindings == nil {
					bindings = map[string]string{}
				}
				bindings[strings.TrimPrefix(name, "--")] = value
			}
		}
	}
	return bindings
}

// The resolver chooses the nearest real help target. No positional argument or
// private option value is copied into the resulting acquisition step.
func inputHelp(argv []string, resolve ActionResolver) []Remedy {
	if resolve == nil || len(argv) < 2 || argv[0] != "devtools" {
		return nil
	}
	args := argv[1:]
	prefix := []string{"devtools"}
	for len(args) > 0 {
		name, _, inline := strings.Cut(args[0], "=")
		if name != "--isolated-root" && name != "--format" {
			break
		}
		n := 1
		if !inline {
			n = 2
		}
		if len(args) < n {
			return nil
		}
		if name == "--isolated-root" {
			prefix = append(prefix, args[:n]...)
		}
		args = args[n:]
	}
	for len(args) > 0 {
		candidate := append(append(append([]string{}, prefix...), "help"), args...)
		if resolve(candidate) == nil {
			return []Remedy{{State: "ready", Argv: candidate, RequiredInputs: []string{}, Effect: "read", Message: "Inspect the accepted input methods before supplying the missing input."}}
		}
		args = args[:len(args)-1]
	}
	return nil
}
