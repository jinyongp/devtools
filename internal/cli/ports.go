package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
)

func portFailure(code string) *protocol.Error {
	return protocol.NewError(code, "The requested port or instance condition is not satisfied.", 3, nil)
}
func (a *App) portStore() (ports.Store, *protocol.Error) {
	d, e := a.dataDirectory()
	return ports.Store{Directory: filepath.Join(filepath.Dir(d), "ports")}, e
}
func portDefaults() ([]int, *protocol.Error) {
	d, e := paths.Current()
	if e != nil {
		return nil, portFailure("invalid_config")
	}
	return ports.DefaultRange(filepath.Join(d.Config, "config.toml"))
}
func portOptions() []Option {
	return append(profileOptions(false), Option{Name: "instance", MinLength: 1, Description: "Instance alias or id:ID."}, Option{Name: "dir", MinLength: 1, Description: "Select a project directory."})
}
func instanceFields() map[string]any {
	return map[string]any{"profile": stringSchema(), "instance_id": stringSchema(), "alias": map[string]any{"type": []string{"string", "null"}}, "directory": stringSchema(), "location_status": map[string]any{"enum": []string{"available", "missing", "unknown"}}}
}
func assignmentFields() map[string]any {
	f := instanceFields()
	f["name"] = stringSchema()
	f["port"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}
	return f
}
func fieldsSchema(f map[string]any) map[string]any {
	keys := []string{}
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return object(f, keys...)
}
func (a *App) registerPorts() {
	for _, action := range []string{"list", "show", "allocate", "check", "release", "prune"} {
		cmd := Command{Name: "port " + action, Description: action + " persistent local TCP assignments.", Options: portOptions()}
		if action != "list" && action != "prune" {
			cmd.Arguments = []Argument{{Name: "name", Required: true, Pattern: project.ProfilePattern}}
		}
		f := assignmentFields()
		switch action {
		case "list":
			cmd.Output = object(map[string]any{"items": map[string]any{"type": "array", "items": fieldsSchema(f)}}, "items")
		case "allocate":
			f["created"] = map[string]any{"type": "boolean"}
			cmd.Output = fieldsSchema(f)
		case "check":
			f["occupancy"] = map[string]any{"enum": []string{"free", "in_use", "unknown"}}
			f["tcp_reachable"] = map[string]any{"type": []string{"boolean", "null"}}
			cmd.Output = fieldsSchema(f)
		case "release":
			cmd.Output = object(map[string]any{"released": map[string]any{"type": "boolean"}}, "released")
		case "prune":
			ret := instanceFields()
			ret["reason"] = stringSchema()
			cmd.Output = object(map[string]any{"removed": map[string]any{"type": "array", "items": fieldsSchema(instanceFields())}, "retained": map[string]any{"type": "array", "items": fieldsSchema(ret)}}, "removed", "retained")
		default:
			cmd.Output = fieldsSchema(f)
		}
		cmd.Run = func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			return a.portCommand(ctx, action, r)
		}
		a.commands = append(a.commands, cmd)
	}
	for _, action := range []string{"list", "show", "name", "move", "remove"} {
		cmd := Command{Name: "instance " + action, Description: action + " a profile execution location.", Options: portOptions()}
		if action == "name" || action == "move" || action == "remove" {
			cmd.Arguments = []Argument{{Name: "name", Required: true}}
		}
		f := instanceFields()
		switch action {
		case "list":
			cmd.Output = object(map[string]any{"items": map[string]any{"type": "array", "items": fieldsSchema(f)}}, "items")
		case "name", "move":
			f["changed"] = map[string]any{"type": "boolean"}
			cmd.Output = fieldsSchema(f)
		case "remove":
			cmd.Output = object(map[string]any{"removed": map[string]any{"type": "boolean"}}, "removed")
		default:
			cmd.Output = fieldsSchema(f)
		}
		cmd.Run = func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			return a.instanceCommand(ctx, action, r)
		}
		a.commands = append(a.commands, cmd)
	}
}

// Resolve the current location only when an explicit instance does not select it.
func portTarget(r Request) (profile, selector, dir string, e *protocol.Error) {
	profile, selector, dir = r.Options["profile"], r.Options["instance"], r.Options["dir"]
	if selector != "" && dir != "" {
		return "", "", "", argumentError("Choose instance or dir.", "instance")
	}
	if selector != "" && profile != "" {
		return profile, selector, "", nil
	}
	if dir == "" {
		dir = "."
	}
	p, e := project.Resolve(dir, "")
	if e != nil {
		return "", "", "", e
	}
	if profile != "" && profile != p.Profile && selector == "" {
		return "", "", "", argumentError("Select an instance for another profile.", "instance")
	}
	if profile == "" {
		profile = p.Profile
	}
	return profile, selector, p.Root, nil
}
func assignmentResult(a ports.Assignment) map[string]any {
	row := instanceResult(a.Instance)
	row["name"], row["port"] = a.Name, a.Port
	return row
}
func instanceResult(i ports.Instance) map[string]any {
	status := "available"
	info, e := os.Stat(i.Directory)
	if errors.Is(e, os.ErrNotExist) || e == nil && !info.IsDir() {
		status = "missing"
	} else if e != nil {
		status = "unknown"
	}
	return map[string]any{"profile": i.Profile, "instance_id": i.ID, "alias": i.Alias, "directory": i.Directory, "location_status": status}
}
func checkRelease(ctx context.Context, s ports.Store, st *ports.State, id string) *protocol.Error {
	if e := (services.Store{Data: filepath.Dir(s.Directory)}).Active(ctx, id); e != nil {
		return e
	}
	for _, a := range st.Assignments {
		if a.ID != id {
			continue
		}
		if e := s.Active(ctx, id, a.Name); e != nil {
			return e
		}
		free, e := ports.Available(a.Port)
		if e != nil {
			return e
		}
		if !free {
			return portFailure("port_in_use")
		}
	}
	return nil
}
func (a *App) portCommand(ctx context.Context, action string, r Request) (any, *protocol.Error) {
	if action == "prune" {
		manager, e := a.serviceStore()
		if e != nil {
			return nil, e
		}
		unlock, err := manager.Lock(ctx)
		if err != nil {
			return nil, portFailure("io_error")
		}
		defer unlock()
	}
	profile, selector, dir, e := portTarget(r)
	if (action == "list" || action == "prune") && r.Options["profile"] != "" && r.Options["instance"] == "" && r.Options["dir"] == "" {
		profile, selector, dir, e = r.Options["profile"], "", "", nil
	}
	if e != nil {
		return nil, e
	}
	s, e := a.portStore()
	if e != nil {
		return nil, e
	}
	var result any
	apply := func(st *ports.State) (bool, *protocol.Error) {
		i := st.Find(profile, selector, dir)
		if action == "list" {
			items := []map[string]any{}
			if selector != "" && i == nil {
				return false, portFailure("instance_not_found")
			}
			for _, v := range st.Assignments {
				if v.Profile == profile && (selector == "" && r.Options["dir"] == "" || i != nil && v.ID == i.ID) {
					items = append(items, assignmentResult(v))
				}
			}
			result = map[string]any{"items": items}
			return false, nil
		}
		if action == "prune" {
			if selector != "" && i == nil {
				return false, portFailure("instance_not_found")
			}
			removed := []map[string]any{}
			retained := []map[string]any{}
			ids := map[string]bool{}
			for _, v := range st.Instances {
				if v.Profile != profile || selector != "" && (i == nil || v.ID != i.ID) || r.Options["dir"] != "" && (i == nil || v.ID != i.ID) {
					continue
				}
				_, err := os.Stat(v.Directory)
				if err == nil {
					continue
				}
				reason := ""
				if !errors.Is(err, os.ErrNotExist) {
					reason = "io_error"
				} else if e := checkRelease(ctx, s, st, v.ID); e != nil {
					reason = e.Code
				}
				if reason != "" {
					row := instanceResult(v)
					row["reason"] = reason
					retained = append(retained, row)
					continue
				}
				ids[v.ID] = true
				removed = append(removed, instanceResult(v))
			}
			instances := []ports.Instance{}
			assignments := []ports.Assignment{}
			for _, v := range st.Instances {
				if !ids[v.ID] {
					instances = append(instances, v)
				}
			}
			for _, v := range st.Assignments {
				if !ids[v.ID] {
					assignments = append(assignments, v)
				}
			}
			st.Instances, st.Assignments = instances, assignments
			result = map[string]any{"removed": removed, "retained": retained}
			return len(removed) > 0, nil
		}
		name := r.Args[0]
		if action == "allocate" {
			root := dir
			if i != nil {
				root = i.Directory
			}
			if root == "" {
				return false, portFailure("instance_not_found")
			}
			p, e := project.Resolve(root, "")
			if e != nil {
				return false, e
			}
			if p.Root != root || p.Profile != profile {
				return false, portFailure("instance_conflict")
			}
			def, ok := p.Ports[name]
			if !ok {
				return false, portFailure("port_not_found")
			}
			if i == nil {
				i, e = st.Register(profile, root)
				if e != nil {
					return false, e
				}
			}
			defaults, e := portDefaults()
			if e != nil {
				return false, e
			}
			v, created, e := st.Allocate(ctx, *i, name, def, defaults)
			if e != nil {
				return false, e
			}
			row := assignmentResult(v)
			row["created"] = created
			result = row
			return created, nil
		}
		if i == nil {
			if action == "release" {
				result = map[string]bool{"released": false}
				return false, nil
			}
			return false, portFailure("instance_not_found")
		}
		v := st.Get(i.ID, name)
		if v == nil {
			if action == "release" {
				result = map[string]bool{"released": false}
				return false, nil
			}
			return false, portFailure("port_not_found")
		}
		switch action {
		case "show":
			result = assignmentResult(*v)
		case "check":
			row := assignmentResult(*v)
			free, e := ports.Available(v.Port)
			row["occupancy"] = "free"
			row["tcp_reachable"] = nil
			if e != nil {
				row["occupancy"] = "unknown"
			} else {
				if !free {
					row["occupancy"] = "in_use"
				}
				row["tcp_reachable"] = ports.Reachable(ctx, v.Port)
			}
			result = row
		case "release":
			if e := s.Active(ctx, i.ID, name); e != nil {
				return false, e
			}
			free, e := ports.Available(v.Port)
			if e != nil {
				return false, e
			}
			if !free {
				return false, portFailure("port_in_use")
			}
			items := []ports.Assignment{}
			for _, x := range st.Assignments {
				if x.ID != i.ID || x.Name != name {
					items = append(items, x)
				}
			}
			st.Assignments = items
			result = map[string]bool{"released": true}
			return true, nil
		}
		return false, nil
	}
	if action == "allocate" || action == "release" || action == "prune" {
		e = s.Update(ctx, apply)
	} else {
		var st *ports.State
		st, e = s.Read()
		if e == nil {
			_, e = apply(st)
		}
	}
	return result, e
}

func (a *App) instanceCommand(ctx context.Context, action string, r Request) (any, *protocol.Error) {
	if action == "move" || action == "remove" {
		manager, e := a.serviceStore()
		if e != nil {
			return nil, e
		}
		unlock, err := manager.Lock(ctx)
		if err != nil {
			return nil, portFailure("io_error")
		}
		defer unlock()
	}
	if (action == "name" || action == "move" || action == "remove") && r.Options["instance"] != "" {
		return nil, argumentError("Use the positional instance name.", "instance")
	}
	if action == "name" && !project.ValidProfile(r.Args[0]) {
		return nil, argumentError("Invalid instance alias.", "name")
	}
	destination := ""
	if action == "move" {
		destination = r.Options["dir"]
		if destination == "" {
			return nil, argumentError("Supply the new directory.", "dir")
		}
		delete(r.Options, "dir")
	}
	if action == "move" || action == "remove" {
		r.Options["instance"] = r.Args[0]
	}
	profile, selector, dir, e := portTarget(r)
	if action == "list" && r.Options["profile"] != "" && r.Options["instance"] == "" && r.Options["dir"] == "" {
		profile, selector, dir, e = r.Options["profile"], "", "", nil
	}
	if e != nil {
		return nil, e
	}
	s, e := a.portStore()
	if e != nil {
		return nil, e
	}
	var result any
	apply := func(st *ports.State) (bool, *protocol.Error) {
		i := st.Find(profile, selector, dir)
		if action == "list" {
			items := []map[string]any{}
			if selector != "" && i == nil {
				return false, portFailure("instance_not_found")
			}
			for _, v := range st.Instances {
				if v.Profile == profile && (selector == "" && r.Options["dir"] == "" || i != nil && v.ID == i.ID) {
					items = append(items, instanceResult(v))
				}
			}
			result = map[string]any{"items": items}
			return false, nil
		}
		if action == "name" {
			alias := r.Args[0]
			existing := st.Find(profile, alias, "")
			if existing != nil && (i == nil || existing.ID != i.ID) {
				return false, portFailure("instance_conflict")
			}
			if i == nil {
				var e *protocol.Error
				i, e = st.Register(profile, dir)
				if e != nil {
					return false, e
				}
			}
			changed := i.Alias == nil || *i.Alias != alias
			i.Alias = &alias
			st.SyncInstance(*i)
			row := instanceResult(*i)
			row["changed"] = changed
			result = row
			return changed, nil
		}
		if i == nil {
			if action == "remove" {
				result = map[string]bool{"removed": false}
				return false, nil
			}
			return false, portFailure("instance_not_found")
		}
		switch action {
		case "show":
			result = instanceResult(*i)
		case "move":
			if e := (services.Store{Data: filepath.Dir(s.Directory)}).Active(ctx, i.ID); e != nil {
				return false, e
			}
			p, e := project.Resolve(destination, "")
			if e != nil {
				return false, e
			}
			if p.Profile != profile {
				return false, portFailure("instance_conflict")
			}
			existing := st.Find(profile, "", p.Root)
			if existing != nil && existing.ID != i.ID {
				return false, portFailure("instance_conflict")
			}
			for _, v := range st.Assignments {
				if v.ID == i.ID {
					if e := s.Active(ctx, i.ID, v.Name); e != nil {
						return false, e
					}
				}
			}
			changed := i.Directory != p.Root
			i.Directory = p.Root
			st.SyncInstance(*i)
			row := instanceResult(*i)
			row["changed"] = changed
			result = row
			return changed, nil
		case "remove":
			if e := (services.Store{Data: filepath.Dir(s.Directory)}).Active(ctx, i.ID); e != nil {
				return false, e
			}
			for _, v := range st.Assignments {
				if v.ID == i.ID {
					return false, portFailure("instance_conflict")
				}
			}
			items := []ports.Instance{}
			for _, v := range st.Instances {
				if v.ID != i.ID {
					items = append(items, v)
				}
			}
			st.Instances = items
			result = map[string]bool{"removed": true}
			return true, nil
		}
		return false, nil
	}
	if action == "list" || action == "show" {
		var st *ports.State
		st, e = s.Read()
		if e == nil {
			_, e = apply(st)
		}
	} else {
		e = s.Update(ctx, apply)
	}
	return result, e
}
