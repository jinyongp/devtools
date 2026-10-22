package cli

import (
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/doctor"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/lifecycle"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

type networkCandidateFields struct {
	Ports                                      map[string]project.Port
	Proxies                                    map[string]project.Proxy
	Backed                                     map[string]bool
	SourceDigest                               string
	Alias                                      string
	DefaultID, DefaultGeneration, ActivationID string
	SnapshotIDs                                map[string]string
}

func (a *App) prepareProxyNetwork(ctx context.Context, p project.Context, names []string, originals []services.Record, overrides map[string]*string, streams IO, id string, selections ...map[string]string) (map[string]*network.ExecutionSnapshot, *protocol.Error) {
	m, e := a.proxyStore()
	if e != nil {
		return nil, e
	}
	hm, e := a.hostsManager(streams)
	if e != nil {
		return nil, e
	}
	directories, pathErr := a.currentPaths()
	if pathErr != nil {
		return nil, portFailure("io_error")
	}
	defaults, e := a.portDefaults()
	if e != nil {
		return nil, e
	}
	var bundleSelections map[string]string
	if len(selections) > 0 {
		bundleSelections = selections[0]
	}
	result, e := (netops.Store{Data: m.Data}).Apply(ctx, id, struct {
		Action, Profile, Root string
		Names                 []string
		Overrides             map[string]*string
		Selections            map[string]string `json:"selections,omitempty"`
	}{"prepare-execution", p.Profile, p.Root, names, overrides, bundleSelections}, []string{"project-network/" + p.Profile + "/" + p.Root}, func(j *netops.Journal) (any, *protocol.Error) {
		var fields networkCandidateFields
		present, e := j.Get("fields", &fields)
		if e != nil {
			return nil, e
		}
		selectedProxies := []string{}
		selectedServices := map[string]bool{}
		commands := []project.Command{}
		for _, name := range names {
			command, ok := p.Commands[name]
			if !ok {
				return nil, portFailure("command_not_found")
			}
			matched := false
			for _, service := range command.Serve {
				selectedServices[service] = true
				for _, route := range p.Proxies {
					if route.Port == service {
						matched = true
					}
				}
			}
			if len(command.Serve) == 0 || !matched {
				return nil, protocol.NewError("proxy_route_required", "--proxy requires a served service with a matching proxy declaration.", 3, map[string]any{"command": name})
			}
			commands = append(commands, command)
		}
		for name, definition := range p.Proxies {
			if selectedServices[definition.Port] {
				selectedProxies = append(selectedProxies, name)
			}
		}
		sort.Strings(selectedProxies)
		if !present {
			st, e := managerPortStore(m).Read()
			if e != nil {
				return nil, e
			}
			alias := ""
			if instance := st.Find(p.Profile, "", p.Root); instance != nil && instance.Alias != nil {
				alias = *instance.Alias
			}
			resolved, e := p.ResolveNetwork(os.Environ(), alias, selectedProxies...)
			if e != nil {
				return nil, e
			}
			services := []string{}
			for service := range selectedServices {
				services = append(services, service)
			}
			resolved.Context, e = resolved.Context.ResolvePorts(os.Environ(), services)
			if e != nil {
				return nil, e
			}
			fields = networkCandidateFields{Ports: map[string]project.Port{}, Proxies: map[string]project.Proxy{}, Backed: resolved.EnvironmentBacked, SourceDigest: resolved.SourceDigest, Alias: alias, DefaultID: tasks.ID(), DefaultGeneration: tasks.ID(), ActivationID: tasks.ID(), SnapshotIDs: map[string]string{}}
			for service := range selectedServices {
				fields.Ports[service] = resolved.Context.Ports[service]
			}
			for _, name := range selectedProxies {
				fields.Proxies[name] = resolved.Context.Proxies[name]
			}
			for _, name := range names {
				fields.SnapshotIDs[name] = tasks.ID()
			}
			if e := j.Set("fields", fields); e != nil {
				return nil, e
			}
		}
		source, e := p.NetworkSources()
		if e != nil {
			return nil, e
		}
		if source != fields.SourceDigest {
			return nil, portFailure("network_snapshot_changed")
		}
		for _, original := range originals {
			if !original.ProxyMode {
				return nil, protocol.NewError("process_conflict", "An existing execution uses a different network mode; stop it before starting with --proxy.", 3, map[string]any{"command": original.Command})
			}
		}
		var snapshots map[string]*network.ExecutionSnapshot
		prepared, e := j.Get("snapshots", &snapshots)
		if e != nil {
			return nil, e
		}
		if !prepared {
			resolvedProject := p
			resolvedProject.Ports = map[string]project.Port{}
			for name, definition := range p.Ports {
				resolvedProject.Ports[name] = definition
			}
			for name, definition := range fields.Ports {
				resolvedProject.Ports[name] = definition
			}
			states := map[string]*values.State{}
			effective := map[string]execution.Command{}
			bindings := map[string]map[string]string{}
			for _, name := range names {
				command, e := execution.ResolveConfigured(p, name, overrides[name], nil)
				if e != nil {
					return nil, e
				}
				if len(selections) > 0 {
					if selected, ok := selections[0][name]; ok {
						command.Selection = selected
					}
				}
				effective[name] = command
				if command.NeedsValues() {
					directory, e := a.dataDirectory()
					if e != nil {
						return nil, e
					}
					observed, e := values.CaptureCurrent(ctx, filepath.Dir(directory), p.Profile, command.Env, false, time.Now(), command.Selection)
					if e != nil {
						return nil, e
					}
					state := observed.StateCopy()
					observed.Destroy()
					if e := state.CheckEnv(command.Env); e != nil {
						return nil, e
					}
					states[name] = state
				}
			}
			snapshots = map[string]*network.ExecutionSnapshot{}
			_, e := managerPortStore(m).AllocateUnion(ctx, resolvedProject, commands, defaults, func(st *ports.State) *protocol.Error {
				instance := st.Find(p.Profile, "", p.Root)
				if instance == nil {
					return portFailure("instance_not_found")
				}
				alias := ""
				if instance.Alias != nil {
					alias = *instance.Alias
				}
				if alias != fields.Alias {
					return portFailure("network_snapshot_changed")
				}
				previous := append([]network.Activation{}, st.Network.Activations...)
				defer func() { st.Network.Activations = previous }()
				activations := []network.Activation{}
				listeners := map[string]string{}
				portNumbers := map[string]int{}
				for service := range selectedServices {
					assignment := st.Get(instance.ID, service)
					if assignment == nil {
						return portFailure("port_not_found")
					}
					portNumbers[service] = assignment.Port
				}
				for _, name := range selectedProxies {
					definition := p.Proxies[name]
					ref := network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: name}
					activation, e := proxy.BuildProjectActivation(st, ref, definition, fields.Proxies[name].Host, fields.SourceDigest, fields.Backed[name])
					if e != nil {
						return e
					}
					if e := st.SetActivation(activation); e != nil {
						return e
					}
					activations = append(activations, activation)
					listeners[activation.Listener] = st.Listener(activation.Listener).Generation
				}
				for _, name := range names {
					command := effective[name]
					bind, e := st.Bind(p, p.Commands[name], command.Env, states[name])
					if e != nil {
						return e
					}
					bindings[name] = bind
					public := map[string]string{}
					for key, definition := range command.Bind {
						if definition.Template == nil {
							public[key] = bind[key]
						}
					}
					snapshots[name] = &network.ExecutionSnapshot{ID: fields.SnapshotIDs[name], InstanceID: instance.ID, Ports: portNumbers, Bindings: public, Activations: activations, Listeners: listeners, SourceDigest: fields.SourceDigest, IsolatedRoot: directories.IsolatedRoot}
				}
				return nil
			}, func(st *ports.State) *protocol.Error {
				for _, name := range selectedProxies {
					if network.ListenerName(p.Proxies[name].Listener) == network.DefaultListener {
						return st.EnsureDefaultListener(fields.DefaultID, fields.DefaultGeneration)
					}
				}
				return nil
			})
			if e != nil {
				return nil, e
			}
			for _, name := range names {
				command := effective[name]
				args := make([]string, len(command.Exec))
				for index, arg := range command.Exec {
					expanded, e := project.Expand(arg, func(key string) (string, bool) {
						if !strings.HasPrefix(key, "bind.") {
							return "", false
						}
						v, ok := bindings[name][strings.TrimPrefix(key, "bind.")]
						return v, ok
					})
					if e != nil {
						return nil, e
					}
					args[index] = expanded
				}
				prepared := execution.Prepared{Command: command, Args: args, State: states[name]}
				if path, ok := bindings[name]["PATH"]; ok {
					prepared.PathOverride = &path
				}
				checks := prepared.Checks(ctx, os.Environ(), true)
				if !execution.ChecksPassed(checks) {
					return nil, doctor.Failure(checks, command.Name, command.Directory, command.Project.Profile, command.Env, command.Selection, "managed", command.Inject)
				}
			}
			if e := j.Set("snapshots", snapshots); e != nil {
				return nil, e
			}
		}
		activations := []network.Activation{}
		if len(names) > 0 {
			activations = snapshots[names[0]].Activations
		}
		if e := m.ActivateProjectBatch(ctx, p, activations, hm, fields.ActivationID); e != nil {
			return nil, e
		}
		return map[string]any{"snapshots": snapshots, "changed": true}, nil
	})
	if e != nil {
		return nil, e
	}
	encoded, _ := json.Marshal(result)
	var decoded struct {
		Snapshots map[string]*network.ExecutionSnapshot `json:"snapshots"`
	}
	if json.Unmarshal(encoded, &decoded) != nil {
		return nil, portFailure("invalid_storage")
	}
	return decoded.Snapshots, nil
}

func (a *App) applyNetworkSnapshot(command execution.Command, snapshot *network.ExecutionSnapshot) (execution.Command, *protocol.Error) {
	command.Network = snapshot
	dependencies, e := a.executionDependencies(command)
	if e != nil {
		return command, e
	}
	if e := execution.ValidateNetwork(command, dependencies); e != nil {
		return command, e
	}
	return command, nil
}

func networkSnapshotPath(data, id string) string {
	return filepath.Join(data, "processes", id, "network.json")
}

func (a *App) networkLifecycle(manager *lifecycle.Manager, streams IO) {
	manager.PrepareNetwork = func(ctx context.Context, request lifecycle.Request, originals []services.Record, id string) (map[string]*network.ExecutionSnapshot, *protocol.Error) {
		names := append([]string{}, request.Commands...)
		selected := []services.Record{}
		overrides := map[string]*string{}
		selections := map[string]string{}
		if request.Action == lifecycle.ActionRestart {
			names = []string{}
			byName := map[string]services.Record{}
			for _, record := range originals {
				byName[record.Command] = record
			}
			for _, name := range request.Commands {
				record, ok := byName[name]
				if !ok || !record.ProxyMode {
					continue
				}
				names = append(names, name)
				selected = append(selected, record)
				env := request.Env
				if env == nil {
					env = record.EnvOverride
				}
				overrides[name] = env
				bundle := request.Bundle
				if bundle == nil {
					bundle = record.BundleOverride
				}
				if bundle != nil {
					selections[name] = *bundle
				}
			}
		} else {
			selected = originals
			for _, name := range names {
				overrides[name] = request.Env
				if request.Bundle != nil {
					selections[name] = *request.Bundle
				}
			}
		}
		return a.prepareProxyNetwork(ctx, request.Project, names, selected, overrides, streams, id, selections)
	}
	manager.SnapshotPreflight = a.preflightSelectedNetworkSnapshot
}

func (a *App) preflightNetworkSnapshot(ctx context.Context, p project.Context, name string, env *string, snapshot *network.ExecutionSnapshot, beforeStop bool) *protocol.Error {
	return a.preflightSelectedNetworkSnapshot(ctx, p, name, env, snapshot, beforeStop, nil)
}
func (a *App) preflightSelectedNetworkSnapshot(ctx context.Context, p project.Context, name string, env *string, snapshot *network.ExecutionSnapshot, beforeStop bool, bundle *string) *protocol.Error {
	current, e := project.Resolve(p.Root, "")
	if e != nil {
		return e
	}
	if current.Root != p.Root || current.Profile != p.Profile {
		return portFailure("instance_conflict")
	}
	command, e := execution.ResolveConfigured(current, name, env, nil)
	if e != nil {
		return e
	}
	if bundle != nil {
		command.Selection = *bundle
	}
	command, e = a.applyNetworkSnapshot(command, snapshot)
	if e != nil {
		return e
	}
	dependencies, e := a.executionDependencies(command)
	if e != nil {
		return e
	}
	if beforeStop {
		return execution.Validate(ctx, command, dependencies, os.Environ())
	}
	return execution.Preflight(ctx, command, dependencies, os.Environ())
}
