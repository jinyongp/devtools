package protocol

import "strings"

// InvocationContext contains only public selection metadata. It never owns
// execution inputs, credentials, stores, or a mutable execution plan.
type InvocationContext struct {
	Operation, Directory, IsolatedRoot, RootSource string
	Kind, Name, Profile, Env, Bundle, Mode         string
	ProfileKnown, EnvKnown, BundleKnown, Inject    bool
}

type ActionRelation uint8

const (
	DiscoveryAction ActionRelation = iota
	SameStoreAction
	DestinationAction
	ManualAction
)

// Action defines scope and effects explicitly instead of deriving permissions
// or side effects from words occurring in argv.
type Action struct {
	Argv            []string
	RequiredInputs  []string
	Message, Effect string
	Relation        ActionRelation
}

func (a Action) Remedy(c InvocationContext) Remedy {
	r := Remedy{Argv: append([]string{}, a.Argv...), RequiredInputs: append([]string{}, a.RequiredInputs...), Message: a.Message, Effect: a.Effect}
	if a.Relation == SameStoreAction && len(r.Argv) > 1 && c.IsolatedRoot != "" {
		found := false
		for _, word := range r.Argv {
			if word == "--isolated-root" || strings.HasPrefix(word, "--isolated-root=") {
				found = true
			}
		}
		if !found {
			r.Argv = append(r.Argv, "--isolated-root", c.IsolatedRoot)
		}
	}
	return r
}

func LocalAction(argv []string, inputs []string, message, effect string) Action {
	return Action{Argv: argv, RequiredInputs: inputs, Message: message, Effect: effect, Relation: SameStoreAction}
}

func (c InvocationContext) Target() map[string]any {
	kind := c.Kind
	if kind == "" {
		kind = "command"
	}
	t := map[string]any{"kind": kind, "name": c.Name}
	if c.ProfileKnown {
		t["profile"] = c.Profile
	}
	if c.EnvKnown {
		t["env"] = c.Env
	}
	if c.BundleKnown {
		t["bundle"] = c.Bundle
	}
	if c.Directory != "" {
		t["directory"] = c.Directory
	}
	if kind == "command" && c.Name != "" {
		t["command"] = c.Name
	}
	return t
}

func (c InvocationContext) DoctorAction() Remedy {
	argv := []string{"devtools", "doctor", "--dir", c.Directory}
	if c.ProfileKnown {
		argv = append(argv, "--profile", c.Profile)
	}
	if c.BundleKnown {
		argv = append(argv, "--bundle", c.Bundle)
	}
	if c.Mode != "" {
		argv = append(argv, "--mode", c.Mode)
	}
	if c.Inject && c.EnvKnown {
		argv = append(argv, "--env", c.Env)
	}
	if c.Name != "" {
		argv = append(argv, c.Name)
	}
	return LocalAction(argv, nil, "Diagnose the same command selection and execution mode.", "read").Remedy(c)
}

// PreserveRecovery carries domain actions across wrappers without copying
// private domain details or replacing an observed effect with a default.
func PreserveRecovery(to, from *Error) {
	if to == nil || from == nil {
		return
	}
	if to.Details == nil {
		to.Details = map[string]any{}
	}
	for _, k := range []string{"remedies", "retry", "effects"} {
		if v, ok := from.Details[k]; ok {
			to.Details[k] = v
		}
	}
}
