package cli

import (
	"context"
	"os"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (a *App) registerGlobalRun() {
	c := Command{Name: "proxy run", Description: "Run a foreground program on an exact global backend port, with public peer URL bindings.", Arguments: []Argument{{Name: "name", Required: true, Pattern: project.ProfilePattern}}, Options: []Option{{Name: "global", Boolean: true, Description: "Select a projectless global route."}}, ChildArgs: true, OutputMode: OutputPassthrough}
	c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		if r.Options["global"] != "true" || len(r.Child) == 0 {
			return nil, argumentError("Use proxy run --global NAME -- PROGRAM ARG...", "")
		}
		store, e := a.portStore()
		if e != nil {
			return nil, e
		}
		dir, err := os.Getwd()
		if err != nil {
			return nil, portFailure("io_error")
		}
		if _, e := process.LookPath(r.Child[0], dir, os.Environ()); e != nil {
			return nil, e
		}
		st, e := store.Read()
		if e != nil {
			return nil, e
		}
		route := st.GlobalRoute(r.Args[0])
		if route == nil {
			return nil, portFailure("route_not_found")
		}
		ref := route.Ref()
		env, release, e := store.PrepareGlobalRun(ctx, r.Args[0])
		if e != nil {
			return nil, e
		}
		defer release()
		m, e := a.proxyStore()
		if e != nil {
			return nil, e
		}
		hm, e := a.hostsManager(streams)
		if e != nil {
			return nil, e
		}
		if _, e := m.RouteUp(ctx, ref, project.Context{}, os.Environ(), hm, tasks.ID()); e != nil {
			return nil, e
		}
		st, e = store.Read()
		if e != nil {
			return nil, e
		}
		current := st.GlobalRoute(r.Args[0])
		if current == nil || current.Ref() != ref {
			return nil, portFailure("route_changed")
		}
		preparedPort := env["PORT"]
		var pending []ports.PendingBinding
		env, pending, e = st.GlobalEnvironment(r.Args[0])
		if e != nil {
			return nil, e
		}
		if len(pending) > 0 || env["PORT"] != preparedPort {
			return nil, portFailure("network_snapshot_changed")
		}
		if roots, e := a.currentPaths(); e == nil && roots.IsolatedRoot != "" {
			env["DEVTOOLS_ISOLATED_ROOT"] = roots.IsolatedRoot
		}
		exit, e := process.Execute(ctx, r.Child, dir, process.Environment(os.Environ(), env), streams.In, streams.Out, streams.Err)
		return processResult{ExitCode: exit}, e
	}
	a.commands = append(a.commands, c)
}
