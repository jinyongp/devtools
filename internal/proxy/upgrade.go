package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/netsetup"
	"github.com/jinyongp/devtools/internal/network"
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
	return upgradeOperationID("devtools/proxy-upgrade/" + m.Build)
}

type proxyUpgradeInput struct {
	Action string
	Build  string
}

const previousHomebrewBuild = "0.46.0/unknown"
const previousArchiveBuild = "0.46.0/516ba782d68179cbdd348020cb3cc7d1af319ac6"

type proxyUpgradeCheckpoint struct {
	Build    string `json:"build"`
	Request  string `json:"request_id"`
	Complete bool   `json:"complete,omitempty"`
}

func (m Manager) upgradeCheckpoint() (proxyUpgradeCheckpoint, *protocol.Error) {
	var checkpoint proxyUpgradeCheckpoint
	err := tasks.ReadPrivate(m.path("upgrade.json"), &checkpoint)
	if errors.Is(err, os.ErrNotExist) {
		return checkpoint, nil
	}
	if err != nil || checkpoint.Build == "" || !protocol.ValidUUID(checkpoint.Request) {
		return proxyUpgradeCheckpoint{}, proxyStorageError()
	}
	return checkpoint, nil
}

// Detect the upgrade before ordinary preparation takes project/network locks.
func (m Manager) NeedsUpgrade(ctx context.Context) (bool, *protocol.Error) {
	if m.Build == "" {
		return false, nil
	}
	checkpoint, e := m.upgradeCheckpoint()
	if e != nil || checkpoint.Request != "" && !checkpoint.Complete {
		return e == nil, e
	}
	if m.RequireCurrentDaemon(ctx) != nil {
		return true, nil
	}
	if checkpoint.Complete {
		return false, nil
	}
	for _, build := range []string{previousHomebrewBuild, previousArchiveBuild} {
		if build == m.Build {
			continue
		}
		id := (Manager{Build: build}).UpgradeRequestID()
		if _, err := os.Lstat(filepath.Join(m.Data, "network-operations", id+".json")); err == nil {
			plan, _, e := m.readInterruptedUpgrade(proxyUpgradeCheckpoint{Build: build, Request: id})
			if e != nil || plan.Previous != "" {
				return e == nil, e
			}
		}
	}
	return false, nil
}

func (m Manager) readInterruptedUpgrade(checkpoint proxyUpgradeCheckpoint) (upgradeProxyPlan, []string, *protocol.Error) {
	var journal netops.Journal
	err := tasks.ReadPrivate(filepath.Join(m.Data, "network-operations", checkpoint.Request+".json"), &journal)
	if errors.Is(err, os.ErrNotExist) {
		return upgradeProxyPlan{}, nil, nil
	}
	input, _ := json.Marshal(proxyUpgradeInput{"proxy-upgrade", checkpoint.Build})
	if err != nil || journal.Fingerprint != fmt.Sprintf("%x", sha256.Sum256(input)) {
		return upgradeProxyPlan{}, nil, proxyStorageError()
	}
	if journal.Done {
		return upgradeProxyPlan{}, nil, nil
	}
	var plan upgradeProxyPlan
	found, e := journal.Get("upgrade", &plan)
	if e != nil || !found {
		return upgradeProxyPlan{}, nil, e
	}
	if !protocol.ValidUUID(plan.Previous) {
		return upgradeProxyPlan{}, nil, proxyStorageError()
	}
	owners := []string{plan.Previous}
	for _, key := range []string{"daemon_attempt", "listener_daemon"} {
		var id string
		if _, e := journal.Get(key, &id); e != nil {
			return upgradeProxyPlan{}, nil, e
		}
		if id != "" {
			owners = append(owners, id)
		}
	}
	return plan, owners, nil
}

func upgradeOperationID(key string) string {
	b := sha256.Sum256([]byte(key))
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
	Previous   string            `json:"previous"`
	LegacyPort int               `json:"legacy_port,omitempty"`
	Listeners  []listenerTarget  `json:"listeners"`
	Exposures  []upgradeExposure `json:"exposures"`
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
	if e != nil {
		return nil, e
	}
	if !protocol.ValidUUID(requestID) {
		return nil, proxyFailure("invalid_argument")
	}
	unlock, err := tasks.Lock(ctx, m.path("installation-upgrade.lock"))
	if err != nil {
		return nil, proxyFailure("proxy_pending")
	}
	defer unlock()
	checkpoint, e := m.upgradeCheckpoint()
	if e != nil {
		return nil, e
	}
	var inherited upgradeProxyPlan
	var owners []string
	if _, err := os.Lstat(filepath.Join(m.Data, "network-operations", requestID+".json")); errors.Is(err, os.ErrNotExist) {
		candidates := []proxyUpgradeCheckpoint{checkpoint}
		if checkpoint.Request == "" {
			for _, build := range []string{previousHomebrewBuild, previousArchiveBuild} {
				if build != m.Build {
					candidates = append(candidates, proxyUpgradeCheckpoint{Build: build, Request: (Manager{Build: build}).UpgradeRequestID()})
				}
			}
		}
		for _, previous := range candidates {
			if previous.Request == "" || previous.Request == requestID || previous.Complete {
				continue
			}
			inherited, owners, e = m.readInterruptedUpgrade(previous)
			if e != nil {
				return nil, e
			}
			if inherited.Previous != "" {
				break
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(m.Data, "network-operations", requestID+".json")); errors.Is(err, os.ErrNotExist) && inherited.Previous == "" {
		status, e := m.Status(ctx)
		if e != nil {
			return nil, e
		}
		if !status.Running && status.State != "unknown" && status.State != "starting" {
			return m.reconcileCurrentUpgrade(ctx)
		}
		if live, err := m.rpc(ctx, "status"); err == nil && live.Build == m.Build && (!enabled || m.networkCapable()) {
			return m.reconcileCurrentUpgrade(ctx)
		}
	}
	result, e := (netops.Store{Data: m.Data}).Apply(ctx, requestID, proxyUpgradeInput{"proxy-upgrade", m.Build}, []string{"exposures", "proxy-daemon"}, func(j *netops.Journal) (any, *protocol.Error) {
		var plan upgradeProxyPlan
		planned, e := j.Get("upgrade", &plan)
		if e != nil {
			return nil, e
		}
		if !planned && inherited.Previous != "" {
			plan = inherited
			status, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			if status.Running {
				live, err := m.rpc(ctx, "status")
				if err != nil || !slices.Contains(owners, live.AttemptID) {
					return nil, proxyFailure("proxy_pending")
				}
				plan.Previous = live.AttemptID
			}
			for i := range plan.Listeners {
				plan.Listeners[i].AttemptID = tasks.ID()
			}
			if e := j.Set("upgrade", plan); e != nil {
				return nil, e
			}
			planned = true
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
			if m.Build != "" && live.Build == m.Build && (!enabled || m.networkCapable()) {
				return map[string]any{"changed": false}, nil
			}
			plan.Previous = live.AttemptID
			st, e := m.portStore().Read()
			if e != nil {
				return nil, e
			}
			if st.Network == nil || len(st.Network.Listeners) == 0 {
				if status.Port == 0 {
					return nil, proxyStorageError()
				}
				plan.LegacyPort = status.Port
			}
			var listeners []network.Listener
			if st.Network != nil {
				listeners = st.Network.Listeners
			}
			for _, definition := range listeners {
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
		if m.Build != "" {
			if err := m.write("upgrade.json", proxyUpgradeCheckpoint{Build: m.Build, Request: requestID}); err != nil {
				return nil, proxyStorageError()
			}
		}
		_, e = j.Get("old_daemon_stopped", &settled)
		if e != nil {
			return nil, e
		}
		if !settled {
			state, e := m.portStore().Read()
			if e != nil {
				return nil, e
			}
			for _, target := range plan.Listeners {
				definition := state.Listener(target.Definition.Name)
				h, t, e := state.ListenerPorts(target.Definition.Name)
				if e != nil || definition == nil || *definition != target.Definition || h != target.HTTPPort || t != target.HTTPSPort {
					return nil, proxyFailure("request_conflict")
				}
			}
			if e := m.checkUpgradeListenerPermissions(plan); e != nil {
				return nil, e
			}
			if _, e := m.stopAttempt(ctx, plan.Previous, func(string, string) *protocol.Error { return nil }); e != nil {
				return nil, e
			}
			release, acquired, err := maintenance.TryLockExclusive(ctx, m.path("lease.lock"))
			if err != nil || !acquired {
				return nil, proxyFailure("proxy_pending")
			}
			e = m.reconcileUnstartedExposures(ctx)
			if e == nil {
				e = j.Set("old_daemon_stopped", true)
			}
			release()
			if e != nil {
				return nil, e
			}
		}
		if plan.LegacyPort != 0 {
			id := upgradeOperationID("devtools/proxy-upgrade-legacy-start/" + requestID)
			started, e := m.Apply(ctx, Request{Action: "start", Port: &plan.LegacyPort, RequestID: id})
			if e != nil {
				return nil, e
			}
			if !started.Running || started.Port != plan.LegacyPort {
				return nil, proxyFailure("proxy_pending")
			}
		} else if e := m.bootNetwork(ctx, j); e != nil {
			return nil, e
		}
		live, err := m.rpc(ctx, "status")
		if err != nil || live.State != "running" || live.AttemptID == plan.Previous || live.Build != m.Build {
			return nil, proxyFailure("proxy_pending")
		}
		var restoredBy string
		if _, e := j.Get("listener_daemon", &restoredBy); e != nil {
			return nil, e
		}
		if restoredBy != "" && restoredBy != live.AttemptID {
			for i := range plan.Listeners {
				plan.Listeners[i].AttemptID = tasks.ID()
			}
			if e := j.Set("upgrade", plan); e != nil {
				return nil, e
			}
		}
		if restoredBy != live.AttemptID {
			if e := j.Set("listener_daemon", live.AttemptID); e != nil {
				return nil, e
			}
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
	restore, captured := outcome["restore"]
	if !ok || !captured {
		return result, nil
	}
	if m.Build != "" {
		if err := m.write("upgrade.json", proxyUpgradeCheckpoint{Build: m.Build, Request: requestID, Complete: true}); err != nil {
			return nil, proxyStorageError()
		}
	}
	raw, err := json.Marshal(restore)
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
		closed, failure := m.closeUpgradeExposure(ctx, selected)
		if failure != nil {
			row["state"], row["reason"] = "recovery_required", failure.Code
			row["next_actions"] = []protocol.Remedy{protocol.LocalAction([]string{"devtools", "expose", "status", item.ID}, nil, "Inspect provider cleanup for this share.", "read").Remedy(protocol.InvocationContext{})}
			continue
		}
		if closed {
			row["state"] = "closed"
			continue
		}
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
		decoded, err := json.Marshal(data["item"])
		var replacement expose.Public
		if err != nil || json.Unmarshal(decoded, &replacement) != nil || replacement.ID == "" {
			return nil, proxyFailure("invalid_storage")
		}
		observed, failure := m.ExposureStatus(ctx, replacement.ID)
		if failure != nil {
			row["state"], row["reason"] = "recovery_required", failure.Code
			continue
		}
		if observed.Generation != replacement.Generation {
			row["state"], row["reason"] = "recovery_required", "request_conflict"
			continue
		}
		replacement = observed
		switch replacement.State {
		case "active":
			row["state"], row["item"] = "restored", replacement
		case "stopped":
			row["state"] = "closed"
		default:
			row["state"], row["reason"], row["item"] = "recovery_required", replacement.Reason, replacement
		}
	}
	outcome["exposures"] = restored
	return outcome, nil
}

func (m Manager) checkUpgradeListenerPermissions(plan upgradeProxyPlan) *protocol.Error {
	if os.Geteuid() == 0 || runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil
	}
	minimum := 1024
	if runtime.GOOS == "linux" {
		if raw, err := os.ReadFile("/proc/sys/net/ipv4/ip_unprivileged_port_start"); err == nil {
			if port, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && port >= 0 {
				minimum = port
			}
		}
	}
	requiresPrivilege := func(port int, host string) bool {
		return port > 0 && port < minimum && (runtime.GOOS != "darwin" || host != "0.0.0.0" && host != "::")
	}
	protected := requiresPrivilege(plan.LegacyPort, "localhost")
	for _, target := range plan.Listeners {
		protected = protected || requiresPrivilege(target.HTTPPort, target.Definition.HTTPHost) || requiresPrivilege(target.HTTPSPort, target.Definition.HTTPSHost)
	}
	if !protected {
		return nil
	}
	if runtime.GOOS == "linux" {
		setup, e := (netsetup.Manager{Data: m.Data}).Check()
		if e != nil {
			return e
		}
		if setup["enabled"] == true {
			return nil
		}
	}
	e := proxyFailure("permission_required")
	e.Details = map[string]any{"effects": "none", "remedies": []protocol.Remedy{
		protocol.LocalAction([]string{"devtools", "proxy", "setup", "--check"}, nil, "Inspect low-port permissions on the installed executable.", "read").Remedy(protocol.InvocationContext{}),
	}}
	return e
}

func exposureStartAbandoned(record expose.Record) bool {
	return (record.State == "preparing" || record.State == "starting") && record.Daemon == "" && record.ProviderOwner == ""
}

// Repair abandoned starts even when the old daemon has already gone away.
// Re-read under the same operation locks that serialize starts and stops.
func (m Manager) reconcileCurrentUpgrade(ctx context.Context) (any, *protocol.Error) {
	repairs, e := m.exposureUpgradeRepairs()
	if e != nil || len(repairs) == 0 {
		return map[string]any{"changed": false}, e
	}
	return (netops.Store{Data: m.Data}).Apply(ctx, tasks.ID(), struct{ Action string }{"repair-exposure-starts"},
		[]string{"exposures", "proxy-daemon"}, func(_ *netops.Journal) (any, *protocol.Error) {
			if e := m.RequireCurrentDaemon(ctx); e != nil {
				return nil, e
			}
			repairs, e := m.exposureUpgradeRepairs()
			if e != nil {
				return nil, e
			}
			changed := false
			for _, record := range repairs {
				_, released, e := m.stopExposure(ctx, record)
				if e != nil {
					return nil, e
				}
				changed = changed || released
			}
			return map[string]any{"changed": changed}, nil
		})
}

func (m Manager) exposureUpgradeRepairs() ([]expose.Record, *protocol.Error) {
	records, e := (expose.Store{Data: m.Data}).List()
	if e != nil || len(records) == 0 {
		return nil, e
	}
	state, e := m.portStore().Read()
	if e != nil {
		return nil, e
	}
	var repairs []expose.Record
	for _, record := range records {
		if exposureStartAbandoned(record) || record.State == "stopped" && record.ReleasePending(state) {
			repairs = append(repairs, record)
		}
	}
	return repairs, nil
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
		if exposureStartAbandoned(record) {
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

// Settle each provider after the listeners are back. The close receipt keeps
// retries distinct from a share already stopped when cleanup began.
func (m Manager) closeUpgradeExposure(ctx context.Context, selected upgradeExposure) (bool, *protocol.Error) {
	id := upgradeOperationID("devtools/proxy-upgrade-exposure-close/" + selected.RequestID)
	outcome, e := (netops.Store{Data: m.Data}).Apply(ctx, id, struct{ Action, ID, Generation string }{
		"upgrade-exposure-close", selected.Item.ID, selected.Item.Generation,
	}, []string{"exposures", "proxy-daemon"}, func(j *netops.Journal) (any, *protocol.Error) {
		store := expose.Store{Data: m.Data}
		record, e := store.Read(selected.Item.ID)
		if e != nil && e.Code == "exposure_not_found" {
			return map[string]any{"closed": true}, nil
		}
		if e != nil {
			return nil, e
		}
		if record.Generation != selected.Item.Generation || record.Route != selected.Item.Route || record.Identity != selected.Identity {
			return nil, expose.Failure("request_conflict")
		}
		var closing bool
		if _, e := j.Get("closing", &closing); e != nil {
			return nil, e
		}
		if !closing && record.State == "stopped" {
			if _, _, e := m.stopExposure(ctx, record); e != nil {
				return nil, e
			}
			return map[string]any{"closed": true}, nil
		}
		if !closing {
			if e := j.Set("closing", true); e != nil {
				return nil, e
			}
		}
		if record.State != "auth_required" || record.EndedAt == nil {
			if _, _, e := m.stopExposure(ctx, record); e != nil {
				return nil, e
			}
		}
		if record.Auth {
			closed, e := store.Read(record.ID)
			if e != nil {
				return nil, e
			}
			closed.State, closed.Reason = "auth_required", "authentication_lost"
			if e := store.Write(closed); e != nil {
				return nil, e
			}
		}
		return map[string]any{"closed": false}, nil
	})
	if e != nil {
		return false, e
	}
	return outcome.(map[string]any)["closed"] == true, nil
}
