package protocol

import (
	"crypto/rand"
	"fmt"
	"reflect"
	"strings"
)

// InvocationContext contains only public selection metadata. It never owns
// execution inputs, credentials, stores, or a mutable execution plan.
type InvocationContext struct {
	Operation, Directory, ConfigPath, IsolatedRoot, RootSource string
	Kind, Name, Profile, Env, Bundle, Mode                     string
	ProfileKnown, EnvKnown, BundleKnown, Inject                bool
	ExecutableOffset                                           *int
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
	// Intent is an optional typed command construction. The consuming CLI
	// registry, not this package, owns its accepted options and static rules.
	Intent *CommandIntent
}

type ActionOption struct{ Name, Value string }
type CommandIntent struct {
	Command    string
	Arguments  []string
	Options    []ActionOption
	Child      []string
	NewRequest bool
}

// NewRequestID is fixed in action data before either renderer sees it.
func NewRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6], b[8] = b[6]&15|64, b[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (a Action) Remedy(c InvocationContext) Remedy {
	r := Remedy{Argv: append([]string{}, a.Argv...), RequiredInputs: append([]string{}, a.RequiredInputs...), Message: a.Message, Effect: a.Effect, Relation: a.Relation}
	if intent := a.Intent; intent != nil {
		r.Argv = append([]string{"devtools"}, strings.Fields(intent.Command)...)
		r.Argv = append(r.Argv, intent.Arguments...)
		for _, option := range intent.Options {
			r.Argv = append(r.Argv, "--"+option.Name+"="+option.Value)
		}
		if intent.NewRequest {
			r.Argv = append(r.Argv, "--request-id", NewRequestID())
		}
		if len(intent.Child) > 0 {
			r.Argv = append(append(r.Argv, "--"), intent.Child...)
		}
	}
	if a.Relation == SameStoreAction && c.ProfileKnown {
		for i, input := range r.RequiredInputs {
			if input == "profile" {
				r.Argv = beforeChild(r.Argv, "--profile", c.Profile)
				r.RequiredInputs = append(r.RequiredInputs[:i], r.RequiredInputs[i+1:]...)
				break
			}
		}
	}
	if a.Relation == SameStoreAction && len(r.Argv) > 1 && r.Argv[0] == "devtools" && c.IsolatedRoot != "" {
		found := false
		for _, word := range r.Argv {
			if word == "--" {
				break
			}
			if word == "--isolated-root" || strings.HasPrefix(word, "--isolated-root=") {
				found = true
			}
		}
		if !found {
			r.Argv = append([]string{"devtools", "--isolated-root", c.IsolatedRoot}, r.Argv[1:]...)
		}
	}
	return r
}

func beforeChild(argv []string, option ...string) []string {
	for i, word := range argv {
		if word == "--" {
			return append(append(append([]string{}, argv[:i]...), option...), argv[i:]...)
		}
	}
	return append(argv, option...)
}

// ActionResolver validates inputs only. It must not invoke handlers, read input,
// unlock a vault, query a provider, or initialize storage.
type ActionResolver func([]string) *Error

// BindGuidance renders declared actions before serialization, including actions
// nested in typed doctor reports and successful workflow results. Unmarked,
// discovery, destination, and external actions never inherit the caller store.
func BindGuidance(value any, c InvocationContext, resolvers ...ActionResolver) any {
	if value == nil {
		return nil
	}
	remedyType := reflect.TypeOf(Remedy{})
	var resolver ActionResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	var bind func(reflect.Value, InvocationContext) reflect.Value
	bind = func(v reflect.Value, scope InvocationContext) reflect.Value {
		if !v.IsValid() {
			return v
		}
		if v.Type() == remedyType {
			r := v.Interface().(Remedy)
			return reflect.ValueOf(prepareAction(r, scope, resolver))
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
			// Public/replayed action maps have lost their Go type. Rebind them
			// too; otherwise old saved templates could silently become ready.
			if m, ok := v.Interface().(map[string]any); ok && m["argv"] != nil && m["required_inputs"] != nil && m["effect"] != nil && m["message"] != nil {
				var r Remedy
				if decodeActionMap(m, &r) {
					out := make(map[string]any, len(m)+2)
					for key, value := range m {
						out[key] = value
					}
					prepared := prepareAction(r, scope, resolver)
					for key, value := range diagnosticObject(prepared) {
						out[key] = value
					}
					return reflect.ValueOf(out)
				}
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
			for i := 0; i < v.NumField(); i++ {
				f := out.Field(i)
				if f.CanSet() && v.Type().Field(i).Tag.Get("json") != "-" {
					f.Set(bind(f, scope))
				}
			}
			return out
		case reflect.Pointer:
			if v.IsNil() {
				return v
			}
			out := reflect.New(v.Type().Elem())
			out.Elem().Set(bind(v.Elem(), scope))
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
	if c.ConfigPath != "" {
		t["config_path"] = c.ConfigPath
	}
	if c.Mode != "" && c.Mode != "foreground" {
		t["mode"] = c.Mode
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
