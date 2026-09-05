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
			a, created, e := st.Allocate(ctx, *i, name, def, defaults)
			if e != nil {
				return false, e
			}
			free, e := Available(a.Port)
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
	out := map[string]string{}
	sec := map[string]bool{}
	if state != nil {
		list, e := state.List(values.Secret, env)
		if e != nil {
			return nil, e
		}
		for _, m := range list {
			sec[m.Key] = true
		}
	}
	keys := []string{}
	for key := range c.Bind {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b := c.Bind[key]
		if sec[key] {
			return nil, fail("binding_conflict")
		}
		if b.Template != nil {
			continue
		}
		profile := b.Profile
		if profile == "" {
			profile = p.Profile
		}
		if profile != p.Profile && b.Instance == "" {
			return nil, fail("binding_reference_error")
		}
		i := st.Find(profile, b.Instance, p.Root)
		if i == nil {
			return nil, fail("instance_not_found")
		}
		info, e := os.Stat(i.Directory)
		if e != nil || !info.IsDir() {
			return nil, fail("instance_not_found")
		}
		a := st.Get(i.ID, b.Port)
		if a == nil {
			return nil, fail("port_not_found")
		}
		out[key] = strconv.Itoa(a.Port)
	}
	for _, key := range keys {
		b := c.Bind[key]
		if b.Template == nil {
			continue
		}
		v, e := project.Expand(*b.Template, func(ref string) (string, bool) {
			if strings.HasPrefix(ref, "bind.") {
				name := strings.TrimPrefix(ref, "bind.")
				binding, ok := c.Bind[name]
				if !ok || binding.Template != nil {
					return "", false
				}
				v, ok := out[name]
				return v, ok
			}
			if strings.HasPrefix(ref, "var.") && state != nil {
				v, _, e := state.GetVariable(strings.TrimPrefix(ref, "var."), env)
				return v, e == nil
			}
			return "", false
		})
		if e != nil {
			return nil, e
		}
		out[key] = v
	}
	return out, nil
}
