package ports

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

type Prepared struct {
	Bind        map[string]string
	Args        []string
	Assignments []Assignment
}

func (s Store) Prepare(ctx context.Context, p project.Context, c project.Command, env string, valuesState *values.State, defaults []int, preview bool) (Prepared, func(), *protocol.Error) {
	result := Prepared{}
	release := func() {}
	change := func(st *State) (bool, *protocol.Error) {
		before := len(st.Instances)
		i := st.Find(p.Profile, "", p.Root)
		if len(c.Serve) > 0 && i == nil {
			var e *protocol.Error
			i, e = st.Register(p.Profile, p.Root)
			if e != nil {
				return false, e
			}
		}
		changed := len(st.Instances) != before
		names := append([]string{}, c.Serve...)
		sort.Strings(names)
		for _, name := range names {
			def, ok := p.Ports[name]
			if !ok {
				return false, fail("port_not_found")
			}
			if e := s.Active(ctx, i.ID, name); e != nil {
				return false, e
			}
			a, created, e := s.Allocate(ctx, st, *i, name, def, defaults)
			if e != nil {
				return false, e
			}
			free, e := s.Available(a.Port)
			if e != nil {
				return false, e
			}
			if !free {
				return false, fail("port_in_use")
			}
			changed = changed || created
			result.Assignments = append(result.Assignments, a)
		}
		bind, e := st.Bind(p, c, env, valuesState)
		if e != nil {
			return false, e
		}
		result.Bind = bind
		result.Args = make([]string, len(c.Exec))
		for n, arg := range c.Exec {
			expanded, e := project.Expand(arg, func(key string) (string, bool) {
				if !strings.HasPrefix(key, "bind.") {
					return "", false
				}
				v, ok := bind[strings.TrimPrefix(key, "bind.")]
				return v, ok
			})
			if e != nil {
				return false, e
			}
			result.Args[n] = expanded
		}
		if !preview && len(names) > 0 {
			var e *protocol.Error
			release, e = s.Claim(ctx, i.ID, names)
			if e != nil {
				return false, e
			}
		}
		return changed, nil
	}
	var e *protocol.Error
	if preview {
		var st *State
		st, e = s.Read()
		if e == nil {
			_, e = change(st)
		}
	} else {
		e = s.Update(ctx, change)
	}
	if e != nil {
		release()
		return Prepared{}, func() {}, e
	}
	return result, release, nil
}
func (st *State) Bind(p project.Context, c project.Command, env string, state *values.State) (map[string]string, *protocol.Error) {
	out, pending, err := st.bind(p, c, env, state, false)
	if err != nil {
		return nil, err
	}
	if len(pending) != 0 {
		return nil, fail("binding_reference_error")
	}
	return out, nil
}

// BindValidation resolves bindings that are already represented in the current
// registry without checking/claiming served-port runtime ownership. A direct
// binding to a port served by this same command may remain pending when a
// config change introduces a new assignment that can only exist after restart.
func (st *State) BindValidation(p project.Context, c project.Command, env string, state *values.State) (map[string]string, map[string]bool, *protocol.Error) {
	return st.bind(p, c, env, state, true)
}

func (st *State) bind(p project.Context, c project.Command, env string, state *values.State, allowPendingServe bool) (map[string]string, map[string]bool, *protocol.Error) {
	out := map[string]string{}
	pending := map[string]bool{}
	sec := map[string]bool{}
	if state != nil {
		list, err := state.List(values.Secret, env)
		if err != nil {
			return nil, nil, err
		}
		for _, metadata := range list {
			sec[metadata.Key] = true
		}
	}
	served := map[string]bool{}
	for _, name := range c.Serve {
		served[name] = true
	}
	pendingSelfServe := func(profile, instance, port string) bool {
		if profile == "" {
			profile = p.Profile
		}
		return allowPendingServe && profile == p.Profile && instance == "" && served[port]
	}

	keys := []string{}
	for key := range c.Bind {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		binding := c.Bind[key]
		if sec[key] {
			return nil, nil, fail("binding_conflict")
		}
		if binding.Template != nil {
			continue
		}
		profile := binding.Profile
		if profile == "" {
			profile = p.Profile
		}
		if profile != p.Profile && binding.Instance == "" {
			return nil, nil, fail("binding_reference_error")
		}
		instance := st.Find(profile, binding.Instance, p.Root)
		if instance == nil {
			if pendingSelfServe(profile, binding.Instance, binding.Port) {
				pending[key] = true
				continue
			}
			return nil, nil, fail("instance_not_found")
		}
		info, err := os.Stat(instance.Directory)
		if err != nil || !info.IsDir() {
			return nil, nil, fail("instance_not_found")
		}
		assignment := st.Get(instance.ID, binding.Port)
		if assignment == nil {
			if pendingSelfServe(profile, binding.Instance, binding.Port) {
				pending[key] = true
				continue
			}
			return nil, nil, fail("port_not_found")
		}
		out[key] = strconv.Itoa(assignment.Port)
	}
	for _, key := range keys {
		binding := c.Bind[key]
		if binding.Template == nil {
			continue
		}
		deferred := false
		value, err := project.Expand(*binding.Template, func(ref string) (string, bool) {
			if strings.HasPrefix(ref, "bind.") {
				name := strings.TrimPrefix(ref, "bind.")
				target, ok := c.Bind[name]
				if !ok || target.Template != nil {
					return "", false
				}
				if pending[name] {
					deferred = true
					return "", true
				}
				value, ok := out[name]
				return value, ok
			}
			if strings.HasPrefix(ref, "var.") && state != nil {
				value, _, err := state.GetVariable(strings.TrimPrefix(ref, "var."), env)
				return value, err == nil
			}
			return "", false
		})
		if err != nil {
			return nil, nil, err
		}
		if deferred {
			pending[key] = true
			continue
		}
		out[key] = value
	}
	return out, pending, nil
}
