package protocol

import (
	"reflect"
	"strings"
)

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
	r := Remedy{Argv: append([]string{}, a.Argv...), RequiredInputs: append([]string{}, a.RequiredInputs...), Message: a.Message, Effect: a.Effect, Relation: a.Relation}
	if a.Relation == SameStoreAction && c.ProfileKnown {
		for i, input := range r.RequiredInputs {
			if input == "profile" {
				r.Argv = append(r.Argv, "--profile", c.Profile)
				r.RequiredInputs = append(r.RequiredInputs[:i], r.RequiredInputs[i+1:]...)
				break
			}
		}
	}
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

// BindGuidance renders declared actions before serialization, including actions
// nested in typed doctor reports and successful workflow results. Unmarked,
// discovery, destination, and external actions never inherit the caller store.
func BindGuidance(value any, c InvocationContext) any {
	if value == nil {
		return nil
	}
	remedyType := reflect.TypeOf(Remedy{})
	var bind func(reflect.Value, InvocationContext) reflect.Value
	bind = func(v reflect.Value, scope InvocationContext) reflect.Value {
		if !v.IsValid() {
			return v
		}
		if v.Type() == remedyType {
			r := v.Interface().(Remedy)
			return reflect.ValueOf((Action{Argv: r.Argv, RequiredInputs: r.RequiredInputs, Message: r.Message, Effect: r.Effect, Relation: r.Relation}).Remedy(scope))
		}
		switch v.Kind() {
		case reflect.Interface:
			if v.IsNil() {
				return v
			}
			out := reflect.New(v.Type()).Elem()
			out.Set(bind(v.Elem(), scope))
			return out
		case reflect.Map:
			if v.IsNil() || v.Type().Key().Kind() != reflect.String {
				return v
			}
			if p := v.MapIndex(reflect.ValueOf("profile").Convert(v.Type().Key())); p.IsValid() {
				if p.Kind() == reflect.Interface {
					p = p.Elem()
				}
				if p.IsValid() && p.Kind() == reflect.String {
					scope.Profile, scope.ProfileKnown = p.String(), true
				}
			}
			out := reflect.MakeMapWithSize(v.Type(), v.Len())
			it := v.MapRange()
			for it.Next() {
				out.SetMapIndex(it.Key(), bind(it.Value(), scope))
			}
			return out
		case reflect.Slice:
			if v.IsNil() {
				return v
			}
			out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
			for i := 0; i < v.Len(); i++ {
				out.Index(i).Set(bind(v.Index(i), scope))
			}
			return out
		case reflect.Struct:
			out := reflect.New(v.Type()).Elem()
			out.Set(v)
			// Only diagnostic containers have action fields. Do not traverse
			// arbitrary resource state, credentials, clocks, or custom marshalers.
			for _, name := range []string{"Remedies", "Checks", "Commands", "Report", "Details"} {
				f := out.FieldByName(name)
				if f.IsValid() && f.CanSet() {
					f.Set(bind(f, scope))
				}
			}
			return out
		default:
			return v
		}
	}
	return bind(reflect.ValueOf(value), c).Interface()
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

func WithEffects(e *Error, effects string) *Error {
	if e == nil {
		return nil
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	if e.Details["effects"] != "unknown" {
		e.Details["effects"] = effects
	}
	return e
}
