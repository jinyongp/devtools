package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func RuntimeBuild(version, commit string) string {
	if version == "" {
		return ""
	}
	return version + "/" + commit
}

func (m Manager) UpgradeRequestID() string {
	b := sha256.Sum256([]byte("devtools/proxy-upgrade/" + m.Build))
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (m Manager) RequireCurrentDaemon(ctx context.Context) *protocol.Error {
	if m.Build == "" {
		return nil
	}
	live, err := m.rpc(ctx, "status")
	if err != nil || live.State != "running" || live.Build == m.Build {
		return nil
	}
	return protocol.NewError("old_daemon_version", "The running proxy daemon belongs to a previous installation. Complete the installation upgrade before changing the network.", 3, map[string]any{
		"remedies": []protocol.Remedy{protocol.LocalAction([]string{"devtools", "update", "--resume"}, nil, "Replace the previous daemon and restore its listeners.", "value_write").Remedy(protocol.InvocationContext{})},
	})
}

type upgradeProxyPlan struct {
	Previous  string            `json:"previous"`
	Listeners []listenerTarget  `json:"listeners"`
	Exposures []upgradeExposure `json:"exposures"`
}

type upgradeExposure struct {
	Item      expose.Public `json:"item"`
	Identity  string        `json:"identity"`
	RequestID string        `json:"request_id"`
}

// UpgradeNetwork replaces the daemon and restores the running listener set.
// The caller must reuse requestID when resuming this upgrade.
func (m Manager) UpgradeNetwork(ctx context.Context, requestID string, hm hosts.Manager) (any, *protocol.Error) {
	enabled, e := m.NetworkEnabled()
	if e != nil || !enabled {
		return map[string]any{"changed": false}, e
	}
	if !protocol.ValidUUID(requestID) {
		return nil, proxyFailure("invalid_argument")
	}
	if _, err := os.Lstat(filepath.Join(m.Data, "network-operations", requestID+".json")); errors.Is(err, os.ErrNotExist) {
		status, e := m.Status(ctx)
		if e != nil {
			return nil, e
		}
		if !status.Running && status.State != "unknown" && status.State != "starting" {
			return map[string]any{"changed": false}, nil
		}
		if live, err := m.rpc(ctx, "status"); err == nil && live.Build == m.Build && m.networkCapable() {
			return map[string]any{"changed": false}, nil
		}
	}
	result, e := (netops.Store{Data: m.Data}).Apply(ctx, requestID, struct {
		Action string
		Build  string
	}{"proxy-upgrade", m.Build}, []string{"exposures", "proxy-daemon"}, func(j *netops.Journal) (any, *protocol.Error) {
		var plan upgradeProxyPlan
		planned, e := j.Get("upgrade", &plan)
		if e != nil {
			return nil, e
		}
		if !planned {
			status, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			if !status.Running {
				if status.State == "unknown" || status.State == "starting" {
					return nil, proxyFailure("proxy_pending")
				}
				return map[string]any{"changed": false}, nil
			}
			live, err := m.rpc(ctx, "status")
			if err != nil || live.State != "running" || live.AttemptID == "" {
				return nil, proxyFailure("proxy_pending")
			}
			if m.Build != "" && live.Build == m.Build && m.networkCapable() {
				return map[string]any{"changed": false}, nil
			}
			plan.Previous = live.AttemptID
			st, e := m.portStore().Read()
			if e != nil {
				return nil, e
			}
			if st.Network == nil {
				return nil, proxyFailure("request_conflict")
			}
			for _, definition := range st.Network.Listeners {
				var record Record
				err := tasks.ReadPrivate(m.path(listenerRecord(definition.ID)), &record)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return nil, proxyStorageError()
				}
				if record.State != "running" || record.EndedAt != nil {
					continue
				}
				h, t, e := st.ListenerPorts(definition.Name)
				if e != nil {
					return nil, e
				}
				plan.Listeners = append(plan.Listeners, listenerTarget{Definition: definition, HTTPPort: h, HTTPSPort: t, AttemptID: tasks.ID(), Create: true})
			}
			records, e := (expose.Store{Data: m.Data}).List()
			if e != nil {
				return nil, e
			}
			for _, record := range records {
				if record.State == "active" && record.Daemon == plan.Previous {
					plan.Exposures = append(plan.Exposures, upgradeExposure{Item: record.Public, Identity: record.Identity, RequestID: tasks.ID()})
				}
			}
			if e := j.Set("upgrade", plan); e != nil {
				return nil, e
			}
		}
		var settled bool
		_, e = j.Get("old_daemon_stopped", &settled)
		if e != nil {
			return nil, e
		}
		if !settled {
			if _, e := m.stopAttempt(ctx, plan.Previous, func(string, string) *protocol.Error { return nil }); e != nil {
				return nil, e
			}
			release, acquired, err := maintenance.TryLockExclusive(ctx, m.path("lease.lock"))
			if err != nil || !acquired {
				return nil, proxyFailure("proxy_pending")
			}
			e := m.reconcileUnstartedExposures(ctx)
			if e == nil {
				e = m.closeUpgradeExposures(ctx, plan.Exposures)
			}
			if e == nil {
				e = j.Set("old_daemon_stopped", true)
			}
			release()
			if e != nil {
				return nil, e
			}
		}
		if e := m.bootNetwork(ctx, j); e != nil {
			return nil, e
		}
		live, err := m.rpc(ctx, "status")
		if err != nil || live.State != "running" || live.AttemptID == plan.Previous || live.Build != m.Build {
			return nil, proxyFailure("proxy_pending")
		}
		items := make([]ListenerStatus, 0, len(plan.Listeners))
		for _, target := range plan.Listeners {
			item, e := m.networkRPC(ctx, networkControlRequest{Action: "start", Target: target})
			if e != nil {
				return nil, e
			}
			if !item.Running || item.Generation != target.AttemptID {
				return nil, proxyFailure("proxy_pending")
			}
			items = append(items, item)
		}
		return map[string]any{"changed": true, "listeners": items, "restore": plan.Exposures}, nil
	})
	if e != nil {
		return nil, e
	}
	outcome, ok := result.(map[string]any)
	if !ok || outcome["restore"] == nil {
		return result, nil
	}
	raw, err := json.Marshal(outcome["restore"])
	var selections []upgradeExposure
	if err != nil || json.Unmarshal(raw, &selections) != nil {
		return nil, proxyFailure("invalid_storage")
	}
	delete(outcome, "restore")
	restored := make([]map[string]any, 0, len(selections))
	for _, selected := range selections {
		item := selected.Item
		row := map[string]any{"id": item.ID, "provider": item.Provider, "previous_url": item.URL}
		restored = append(restored, row)
		if item.Auth {
			closed, e := (expose.Store{Data: m.Data}).Read(item.ID)
			if e == nil && closed.Generation == item.Generation && closed.State == "stopped" {
				row["state"] = "closed"
				continue
			}
			row["state"], row["reason"] = "auth_required", "authentication_lost"
			row["next_actions"] = []protocol.Remedy{protocol.LocalAction([]string{"devtools", "expose", "status", item.ID}, nil, "Inspect the share that needs authentication.", "read").Remedy(protocol.InvocationContext{})}
			continue
		}
		_, _, identity, failure := m.exposureIdentity(ctx, item.Route)
		if failure != nil || identity != selected.Identity {
			row["state"], row["reason"] = "recovery_required", "exposure_identity_changed"
			continue
		}
		p := project.Context{}
		if item.Route.Scope == "project" {
			p, failure = profilekey.ResolveProject(m.Data, item.Directory, "")
		}
		var value any
		if failure == nil {
			alias := ""
			if item.Provider == "lan" {
				alias = item.Host
			}
			value, failure = m.ExposeStart(ctx, ExposureRequest{Route: item.Route, Provider: item.Provider, Alias: alias, NoAuth: true, ServePort: item.ServePort, RequestID: selected.RequestID, ExpectedIdentity: selected.Identity}, p, os.Environ(), hm)
		}
		if failure != nil {
			row["state"], row["reason"] = "recovery_required", failure.Code
			continue
		}
		data, ok := value.(map[string]any)
		if !ok || data["item"] == nil {
			return nil, proxyFailure("invalid_storage")
		}
		row["state"], row["item"] = "restored", data["item"]
	}
	outcome["exposures"] = restored
	return outcome, nil
}

// Only starts that have not acquired provider ownership can be cancelled here.
// Own the daemon lease throughout so the record cannot become active meanwhile.
func (m Manager) reconcileUnstartedExposures(ctx context.Context) *protocol.Error {
	store := expose.Store{Data: m.Data}
	records, e := store.List()
	if e != nil {
		return e
	}
	for _, record := range records {
		if (record.State == "preparing" || record.State == "starting") && record.Daemon == "" && record.ProviderOwner == "" {
			if _, e := m.stopDetachedExposure(ctx, record); e != nil {
				return e
			}
		} else if record.State != "stopped" {
			continue
		}
		if _, e := m.releaseExposure(ctx, record); e != nil {
			return e
		}
	}
	return nil
}

func (m Manager) closeUpgradeExposures(ctx context.Context, selections []upgradeExposure) *protocol.Error {
	store := expose.Store{Data: m.Data}
	for _, selected := range selections {
		record, e := store.Read(selected.Item.ID)
		if e != nil {
			return e
		}
		if record.Generation != selected.Item.Generation || record.Identity != selected.Identity {
			return expose.Failure("request_conflict")
		}
		if record.State != "auth_required" || record.EndedAt == nil {
			if _, e := m.stopDetachedExposure(ctx, record); e != nil {
				return e
			}
		}
		if _, e := m.releaseExposure(ctx, record); e != nil {
			return e
		}
		if record.Auth {
			closed, e := store.Read(record.ID)
			if e != nil {
				return e
			}
			closed.State, closed.Reason = "auth_required", "authentication_lost"
			if e := store.Write(closed); e != nil {
				return e
			}
		}
	}
	return nil
}
