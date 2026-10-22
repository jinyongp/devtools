package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func preparationMode(options map[string]string) (network.PreparationMode, *protocol.Error) {
	var proxy, noProxy *bool
	if value, ok := options["proxy"]; ok {
		enabled := value == "true"
		proxy = &enabled
	}
	if value, ok := options["no-proxy"]; ok {
		disabled := value == "true"
		noProxy = &disabled
	}
	return network.PreparationOptions(proxy, noProxy)
}

func (a *App) needsProxyPreparation(p project.Context, names []string, mode network.PreparationMode) (bool, *protocol.Error) {
	if mode == network.PrepareOff {
		return false, nil
	}
	selection, e := p.SelectProxies(names)
	if e != nil {
		return false, e
	}
	if len(selection.Routes) == 0 {
		if mode == network.PrepareOn {
			return false, protocol.NewError("proxy_route_required", "--proxy requires a served service with a matching proxy declaration.", 3, nil)
		}
		return false, nil
	}
	if mode == network.PrepareOn {
		return true, nil
	}
	m, e := a.proxyStore()
	if e != nil {
		return false, e
	}
	st, e := managerPortStore(m).Read()
	if e != nil {
		return false, e
	}
	instance := st.Find(p.Profile, "", p.Root)
	if instance == nil {
		return true, nil
	}
	for _, name := range selection.Routes {
		ref := network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: name}
		if activation := st.Activation(ref); activation == nil || activation.Mode != "disabled" {
			return true, nil
		}
	}
	return false, nil
}

// prepareManagedNetwork keeps the legacy path for existing receipts. New intent
// and child journal locks are released before the caller enters services.Apply.
func (a *App) prepareManagedNetwork(ctx context.Context, p project.Context, q *services.Request, original *services.Record, mode network.PreparationMode, streams IO) *protocol.Error {
	s, e := a.serviceStore()
	if e != nil {
		return e
	}
	store := netops.Store{Data: s.Data}
	exists, intent, e := store.ExecutionKind(q.RequestID)
	if e != nil {
		return e
	}
	recorded, e := s.HasRequest(q.RequestID)
	if e != nil {
		return e
	}
	name := q.Command
	if original != nil {
		name = original.Command
	}
	legacy := !intent && (exists || recorded)
	env, bundle := q.Env, q.Bundle
	if original != nil {
		if env == nil {
			env = original.EnvOverride
		}
		if bundle == nil {
			bundle = original.BundleOverride
		}
	}
	if legacy {
		if mode != network.PrepareOn && original == nil {
			return nil
		}
		originals := []services.Record(nil)
		if original != nil {
			originals = append(originals, *original)
		}
		snapshots, e := a.prepareProxyNetwork(ctx, p, []string{name}, originals, map[string]*string{name: env}, streams, q.RequestID, selectedBundleOverride(name, bundle))
		if e != nil {
			return e
		}
		q.ProxyMode = true
		q.Network = snapshots[name]
		return nil
	}
	input := struct {
		Profile, Root string
		Request       services.Request
	}{p.Profile, p.Root, *q}
	plan, e := store.ChooseExecution(ctx, q.RequestID, input, mode, func() (netops.ExecutionPlan, *protocol.Error) {
		if original != nil {
			return netops.ExecutionPlan{Commands: []string{name}}, nil
		}
		if mode == network.PrepareOn {
			if _, e := a.needsProxyPreparation(p, []string{name}, mode); e != nil {
				return netops.ExecutionPlan{}, e
			}
		}
		// An existing execution keeps its recorded mode when auto is omitted.
		records, e := s.List(ctx, p.Profile)
		if e != nil {
			return netops.ExecutionPlan{}, e
		}
		for _, record := range records {
			if record.Directory == p.Root && record.Command == name && record.EndedAt == nil && record.State != "interrupted" {
				if mode != network.PrepareAuto && record.ProxyMode != (mode == network.PrepareOn) {
					return netops.ExecutionPlan{}, protocol.NewError("process_conflict", "Stop the existing execution before changing its network mode.", 3, nil)
				}
				var snapshot *network.ExecutionSnapshot
				if record.ProxyMode {
					snapshot = &network.ExecutionSnapshot{}
					if e := tasks.ReadPrivate(networkSnapshotPath(s.Data, record.ID), snapshot); e != nil {
						return netops.ExecutionPlan{}, protocol.NewError("invalid_storage", "Cannot read the active execution network snapshot.", 3, nil)
					}
				}
				return netops.ExecutionPlan{Reused: map[string]*network.ExecutionSnapshot{name: snapshot}, ReuseIDs: map[string]string{name: record.ID}}, nil
			}
		}
		needed, e := a.needsProxyPreparation(p, []string{name}, mode)
		if e != nil {
			return netops.ExecutionPlan{}, e
		}
		if needed {
			return netops.ExecutionPlan{Commands: []string{name}}, nil
		}
		return netops.ExecutionPlan{}, nil
	})
	if e != nil {
		return e
	}
	if snapshot, reused := plan.Reused[name]; reused {
		q.ProxyMode = snapshot != nil
		q.Network = snapshot
		q.BeforeStart = func(ctx context.Context) *protocol.Error {
			record, e := s.Status(ctx, plan.ReuseIDs[name])
			if e != nil {
				return e
			}
			if record.EndedAt != nil || record.State == "interrupted" {
				return protocol.NewError("process_conflict", "The reused execution ended before this start completed; use a new request UUID.", 3, nil)
			}
			return nil
		}
		return nil
	}
	if len(plan.Commands) == 0 {
		return nil
	}
	originals := []services.Record(nil)
	if original != nil {
		originals = append(originals, *original)
	}
	snapshots, e := a.prepareNetwork(ctx, p, plan.Commands, originals, map[string]*string{name: env}, streams, plan.NetworkID, mode, false, selectedBundleOverride(name, bundle))
	if e != nil {
		return e
	}
	q.ProxyMode = true
	q.Network = snapshots[name]
	return nil
}
