package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type ExposureRequest struct {
	Route                 network.RouteRef
	Provider, Alias, Auth string
	NoAuth                bool
	ServePort             int
	RequestID             string
	ExpectedIdentity      string
}
type exposureRPCRequest struct{ Action, ID, Generation, Auth string }
type exposureRPCResult struct {
	Item     expose.Public   `json:"item"`
	Error    *protocol.Error `json:"error,omitempty"`
	ExitCode int             `json:"error_exit_code,omitempty"`
}

// Only a failure observed before ingress publication is eligible for automatic
// cleanup. Transport and provider commit failures keep their recovery records.
func exposureStartCanRetire(record expose.Record, code string) bool {
	if record.State != "recovery_required" || record.Reason != code {
		return false
	}
	if record.ProviderOwner == "" {
		return record.Provider == "cloudflared" || record.Provider == "tailscale"
	}
	return record.Provider == "cloudflared" && (code == "cloudflared_exited" || code == "cloudflared_exited_without_url" || code == "cloudflared_url_timeout")
}

func (m Manager) finishFailedExposureStart(ctx context.Context, record expose.Record, failure *receiptError) (any, *protocol.Error) {
	store := expose.Store{Data: m.Data}
	_, _, e := m.stopExposure(ctx, record)
	if e != nil && e.Code != "exposure_not_found" {
		return nil, e
	}
	if e != nil {
		st, readErr := m.portStore().Read()
		if readErr != nil {
			return nil, readErr
		}
		if record.ReleasePending(st) {
			return nil, expose.Failure("exposure_recovery_required")
		}
	}
	// The caller has already persisted the failure and holds the exposure lock.
	// A missing record on replay means the last removal was already applied.
	if err := os.Remove(store.Path(record.ID)); err != nil && !os.IsNotExist(err) {
		return nil, proxyStorageError()
	}
	return map[string]any{"failure": failure}, nil
}

func (m Manager) exposureIdentity(ctx context.Context, ref network.RouteRef) (Item, network.Listener, string, *protocol.Error) {
	snapshot := &routeSnapshot{ref: ref}
	items, e := NewResolver(m.portStore()).list(ctx, "", snapshot)
	if e != nil {
		return Item{}, network.Listener{}, "", e
	}
	st := snapshot.state
	var selected Item
	found := false
	for _, item := range items {
		if item.Ref() == ref && item.Kind == "route" {
			selected = item
			found = true
		}
	}
	if !found || selected.Status != StatusReady || selected.Host == nil || selected.TargetPort == nil {
		return selected, network.Listener{}, "", proxyFailure("route_unavailable")
	}
	l := st.Listener(network.ListenerName(selected.Listener))
	if l == nil {
		return selected, network.Listener{}, "", proxyFailure("listener_not_found")
	}
	h, t, e := st.ListenerPorts(l.Name)
	if e != nil {
		return selected, *l, "", e
	}
	if snapshot.sourceErr != nil {
		return selected, *l, "", snapshot.sourceErr
	}
	digest := project.Digest(struct {
		Item        Item
		Listener    network.Listener
		HTTP, HTTPS int
		Source      string
	}{selected, *l, h, t, snapshot.source})
	return selected, *l, digest, nil
}

func (m Manager) ExposeStart(ctx context.Context, q ExposureRequest, p project.Context, parent []string, hm hosts.Manager) (any, *protocol.Error) {
	if !q.Route.Valid() || q.Provider != "lan" && q.Provider != "cloudflared" && q.Provider != "tailscale" {
		return nil, expose.Failure("invalid_argument")
	}
	if e := expose.ValidateAuth(q.Auth, q.NoAuth); e != nil {
		return nil, e
	}
	if e := m.RequireCurrentDaemon(ctx); e != nil {
		return nil, e
	}
	policy := p.Proxies[q.Route.Proxy].HostPolicy.Canonical()
	if q.Route.Scope == "global" {
		st, err := m.portStore().Read()
		if err != nil {
			return nil, err
		}
		if st.Network != nil {
			for _, route := range st.Network.Routes {
				if route.Ref() == q.Route {
					policy = route.HostPolicy.Canonical()
				}
			}
		}
	}
	store := expose.Store{Data: m.Data}
	commitment, e := store.Commitment(ctx, q.RequestID, q.Auth, q.NoAuth)
	if e != nil {
		return nil, e
	}
	result, e := (netops.Store{Data: m.Data}).Apply(ctx, q.RequestID, struct {
		Action                      string
		Route                       network.RouteRef
		Provider, Alias, Commitment string
		NoAuth                      bool
		ServePort                   int
		HostPolicy                  network.HostPolicy `json:",omitempty"`
		ExpectedIdentity            string             `json:",omitempty"`
	}{"expose-start", q.Route, q.Provider, q.Alias, commitment, q.NoAuth, q.ServePort, policy, q.ExpectedIdentity}, []string{"exposure-route/" + q.Route.Key(), "exposures"}, func(j *netops.Journal) (any, *protocol.Error) {
		var record expose.Record
		present, e := j.Get("exposure", &record)
		if e != nil {
			return nil, e
		}
		var failure receiptError
		if failed, e := j.Get("start_failure", &failure); e != nil {
			return nil, e
		} else if failed {
			if !present {
				return nil, expose.Failure("invalid_storage")
			}
			return m.finishFailedExposureStart(ctx, record, &failure)
		}
		var prepareID string
		prepared, e := j.Get("prepare_id", &prepareID)
		if e != nil {
			return nil, e
		}
		if !prepared {
			prepareID = tasks.ID()
			if e := j.Set("prepare_id", prepareID); e != nil {
				return nil, e
			}
		}
		if !present {
			// DNS and listener preparation stays in the initiating CLI, where OS
			// authorization can be obtained. The daemon never runs an OS helper.
			_, _, _, readyErr := m.exposureIdentity(ctx, q.Route)
			if readyErr != nil {
				if q.ExpectedIdentity != "" {
					return nil, expose.Failure("exposure_identity_changed")
				}
				if _, e := m.RouteUp(ctx, q.Route, p, parent, hm, prepareID); e != nil {
					return nil, e
				}
			}
			item, listener, digest, e := m.exposureIdentity(ctx, q.Route)
			if e != nil {
				return nil, e
			}
			if q.ExpectedIdentity != "" && digest != q.ExpectedIdentity {
				return nil, expose.Failure("exposure_identity_changed")
			}
			if item.HostPolicy.Canonical() != policy {
				return nil, proxyFailure("activation_required")
			}
			record = expose.Record{Public: expose.Public{ID: tasks.ID(), Route: q.Route, Provider: q.Provider, State: "preparing", Generation: tasks.ID(), Listener: listener.Name, Host: *item.Host, Auth: !q.NoAuth, ServePort: q.ServePort, CreatedAt: time.Now().UTC()}, Identity: digest}
			record.Directory = p.Root
			service := ""
			if item.Service != nil {
				service = *item.Service
			}
			record.Pin = network.Pin{ID: record.ID, Route: q.Route, ListenerID: listener.ID, ListenerGeneration: listener.Generation, NetworkDigest: digest, Port: *item.TargetPort, Provider: q.Provider, Service: service, HostPolicy: policy}
			if q.Provider == "lan" {
				if q.Alias != "" {
					record.Host = strings.ToLower(q.Alias)
				} else {
					record.Host = strings.TrimSuffix(*item.Host, ".localhost") + ".local"
				}
				if !network.ValidHost(record.Host) || !strings.HasSuffix(record.Host, ".local") {
					return nil, expose.Failure("invalid_lan_alias")
				}
				items, e := NewResolver(m.portStore()).List(ctx, "")
				if e != nil {
					return nil, e
				}
				for _, item := range items {
					if item.Host != nil && *item.Host == record.Host && item.Ref() != q.Route {
						return nil, expose.Failure("host_conflict")
					}
				}
				st, e := m.portStore().Read()
				if e != nil {
					return nil, e
				}
				hp, tp, e := st.ListenerPorts(listener.Name)
				if e != nil {
					return nil, e
				}
				record.URL, _ = network.RouteURL(record.Host, hp, tp)
				if e := validateLANListener(listener, hp, tp); e != nil {
					return nil, e
				}
				record.ClientSetup = []string{"Configure client DNS for " + record.Host + " using this computer's reachable LAN address; devtools does not advertise mDNS.", "Allow the configured listener port through your host firewall for the intended peers."}
				if tp > 0 {
					record.ClientSetup = append(record.ClientSetup, "Export the public CA with devtools proxy ca export --output FILE, then install it in each client's trust store.")
				}
				record.ClientSetup = append(record.ClientSetup, "With WSL, Windows browser DNS and trust are configured separately from Linux hosts and trust.")
			} else {
				record.Reservation = "origin." + strings.ReplaceAll(record.ID, "-", "")
			}
			if e := j.Set("exposure", record); e != nil {
				return nil, e
			}
		}
		if existing, e := store.Read(record.ID); e == nil {
			if existing.Generation != record.Generation {
				return nil, expose.Failure("request_conflict")
			}
			if existing.State == "active" {
				item, e := m.exposureRPC(ctx, exposureRPCRequest{Action: "status", ID: record.ID, Generation: record.Generation})
				if e != nil {
					return nil, e
				}
				return map[string]any{"item": item, "changed": true}, nil
			}
			if existing.State == "auth_required" || existing.State == "stopped" || existing.ProviderOwner != "" {
				return nil, expose.Failure("exposure_recovery_required")
			}
		} else if e.Code != "exposure_not_found" {
			return nil, e
		}
		if e := store.Write(record); e != nil {
			return nil, e
		}
		if e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			if st.Network == nil {
				return false, proxyFailure("listener_not_found")
			}
			for _, pin := range st.Network.Pins {
				if pin.ID == record.ID {
					if pin != record.Pin {
						return false, proxyFailure("request_conflict")
					}
					return false, nil
				}
			}
			if record.Reservation != "" {
				if existing := st.Reservation(record.Reservation); existing != nil {
					record.OriginPort = existing.Port
				} else {
					for number := 34000; number <= 39999; number++ {
						used := false
						for _, a := range st.Assignments {
							used = used || a.Port == number
						}
						for _, r := range st.Reservations {
							used = used || r.Port == number
						}
						if used {
							continue
						}
						free, e := m.portStore().Available(number)
						if e != nil {
							return false, e
						}
						if !free {
							continue
						}
						if _, e := st.Reference(record.Reservation, number); e != nil {
							return false, e
						}
						record.OriginPort = number
						break
					}
					if record.OriginPort == 0 {
						return false, proxyFailure("port_in_use")
					}
				}
			}
			st.Network.Pins = append(st.Network.Pins, record.Pin)
			return true, nil
		}); e != nil {
			return nil, e
		}
		if record.Reservation != "" {
			st, e := m.portStore().Read()
			if e != nil {
				return nil, e
			}
			r := st.Reservation(record.Reservation)
			if r == nil {
				return nil, proxyFailure("port_not_found")
			}
			record.OriginPort = r.Port
		}
		record.State = "starting"
		if e := store.Write(record); e != nil {
			return nil, e
		}
		var listenerID string
		known, e := j.Get("listener_request", &listenerID)
		if e != nil {
			return nil, e
		}
		if !known {
			listenerID = tasks.ID()
			if e := j.Set("listener_request", listenerID); e != nil {
				return nil, e
			}
		}
		if _, e := m.NetworkApply(ctx, "start", record.Listener, false, listenerID); e != nil {
			return nil, e
		}
		active, e := m.exposureRPC(ctx, exposureRPCRequest{Action: "start", ID: record.ID, Generation: record.Generation, Auth: q.Auth})
		if e != nil {
			current, readErr := store.Read(record.ID)
			if readErr == nil && current.Generation == record.Generation && exposureStartCanRetire(current, e.Code) {
				failure := receiptError{Code: e.Code, Message: e.Message, ExitCode: e.ExitCode}
				if saveErr := j.Set("start_failure", failure); saveErr != nil {
					return nil, saveErr
				}
				if result, cleanupErr := m.finishFailedExposureStart(ctx, current, &failure); cleanupErr == nil {
					return result, nil
				}
			}
			return nil, e
		}
		return map[string]any{"item": active, "changed": true}, nil
	})
	if e != nil {
		return result, e
	}
	// A receipt is historical. Reading current metadata never restores lost auth.
	if value, ok := result.(map[string]any); ok {
		if failed, ok := value["failure"]; ok {
			var failure receiptError
			encoded, err := json.Marshal(failed)
			if err != nil || json.Unmarshal(encoded, &failure) != nil || failure.Code == "" || failure.ExitCode <= 0 {
				return nil, expose.Failure("invalid_storage")
			}
			return nil, protocol.NewError(failure.Code, failure.Message, failure.ExitCode, nil)
		}
		encoded, _ := json.Marshal(value["item"])
		var item expose.Public
		if json.Unmarshal(encoded, &item) == nil {
			if current, e := m.ExposureStatus(ctx, item.ID); e == nil {
				value["item"] = current
			}
		}
	}
	return result, nil
}

func (m Manager) ExposureStatus(ctx context.Context, id string) (expose.Public, *protocol.Error) {
	r, e := (expose.Store{Data: m.Data}).Read(id)
	if e != nil {
		return expose.Public{}, e
	}
	if r.State == "active" || r.State == "starting" {
		item, e := m.exposureRPC(ctx, exposureRPCRequest{Action: "status", ID: id, Generation: r.Generation})
		if e == nil {
			return item, nil
		}
		r.State = "recovery_required"
		r.Reason = e.Code
		if mismatch := m.RequireCurrentDaemon(ctx); mismatch != nil {
			r.Reason = mismatch.Code
		} else if e.Code == "proxy_pending" || e.Code == "proxy_unavailable" {
			r.Reason = "daemon_unavailable"
			if r.Auth {
				daemon, failure := m.Status(ctx)
				if failure == nil && (daemon.State == "interrupted" || daemon.State == "stopped") {
					r.State = "auth_required"
					r.Reason = "authentication_lost"
				}
			}
		}
	}
	if r.State == "stopped" {
		st, e := m.portStore().Read()
		if e != nil {
			return expose.Public{}, e
		}
		item, _ := exposureCurrent(r, st)
		return item, nil
	}
	return r.Public, nil
}
func (m Manager) ExposureList(ctx context.Context) ([]expose.Public, *protocol.Error) {
	records, e := (expose.Store{Data: m.Data}).List()
	if e != nil {
		return nil, e
	}
	out := []expose.Public{}
	for _, r := range records {
		item, e := m.ExposureStatus(ctx, r.ID)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, nil
}

func (m Manager) ExposeStop(ctx context.Context, id, requestID string) (any, *protocol.Error) {
	return (netops.Store{Data: m.Data}).Apply(ctx, requestID, struct{ Action, ID string }{"expose-stop", id}, []string{"exposures", "proxy-daemon"}, func(j *netops.Journal) (any, *protocol.Error) {
		var r expose.Record
		present, e := j.Get("target", &r)
		if e != nil {
			return nil, e
		}
		if !present {
			r, e = (expose.Store{Data: m.Data}).Read(id)
			if e != nil {
				return nil, e
			}
			if e := j.Set("target", r); e != nil {
				return nil, e
			}
		}
		item, changed, e := m.stopExposure(ctx, r)
		if e != nil {
			return nil, e
		}
		return map[string]any{"item": item, "changed": changed}, nil
	})
}

// The caller holds the exposure operation lock.
func (m Manager) stopExposure(ctx context.Context, target expose.Record) (expose.Public, bool, *protocol.Error) {
	record, e := (expose.Store{Data: m.Data}).Read(target.ID)
	if e != nil {
		return expose.Public{}, false, e
	}
	if record.Generation != target.Generation || record.Route != target.Route {
		return expose.Public{}, false, expose.Failure("request_conflict")
	}
	item := record.Public
	wasRunning := record.State != "stopped"
	if wasRunning {
		item, e = m.exposureRPC(ctx, exposureRPCRequest{Action: "stop", ID: record.ID, Generation: record.Generation})
		if e != nil {
			// An unreachable control endpoint does not prove loss of ingress.
			release, acquired, err := maintenance.TryLockExclusive(ctx, m.path("lease.lock"))
			if err != nil || !acquired {
				return expose.Public{}, false, e
			}
			defer release()
			record, e = (expose.Store{Data: m.Data}).Read(target.ID)
			if e != nil {
				return expose.Public{}, false, e
			}
			if record.Generation != target.Generation || record.Route != target.Route {
				return expose.Public{}, false, expose.Failure("request_conflict")
			}
			item, e = m.stopDetachedExposure(ctx, record)
			if e != nil {
				return expose.Public{}, false, e
			}
		}
		if item.ID != record.ID || item.Generation != record.Generation || item.State != "stopped" {
			return expose.Public{}, false, expose.Failure("exposure_recovery_required")
		}
	}
	removed, e := m.releaseExposure(ctx, record)
	return item, wasRunning || removed, e
}

// Called by the daemon with its runtime mutex held, or with the daemon lease held
// by the stopping command. No provider session for this generation can start here.
func (m Manager) stopDetachedExposure(ctx context.Context, record expose.Record) (expose.Public, *protocol.Error) {
	if record.State == "stopped" {
		return record.Public, nil
	}
	if record.Provider != "lan" && record.ProviderOwner != "" {
		provider := m.ExposureProviders[record.Provider]
		if provider == nil {
			switch record.Provider {
			case "cloudflared":
				provider = expose.Cloudflared{Data: m.Data}
			case "tailscale":
				provider = expose.Tailscale{Data: m.Data}
			}
		}
		recovery, ok := provider.(interface {
			RecoverStop(context.Context, expose.Record) *protocol.Error
		})
		if !ok {
			return record.Public, expose.Failure("provider_recovery_required")
		}
		if e := recovery.RecoverStop(ctx, record); e != nil {
			return record.Public, e
		}
	}
	now := time.Now().UTC()
	record.State, record.Reason, record.EndedAt = "stopped", "stop_requested", &now
	return record.Public, (expose.Store{Data: m.Data}).Write(record)
}

func (m Manager) exposureRPC(ctx context.Context, q exposureRPCRequest) (expose.Public, *protocol.Error) {
	var control proxyControl
	if tasks.ReadPrivate(m.path("control.json"), &control) != nil || control.Protocol != networkControlVersion || len(control.Token) != 64 {
		return expose.Public{}, proxyFailure("proxy_unavailable")
	}
	u, e := url.Parse(control.Address)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return expose.Public{}, proxyFailure("proxy_unavailable")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 {
		return expose.Public{}, proxyFailure("proxy_unavailable")
	}
	encoded, _ := json.Marshal(q)
	request, e := http.NewRequestWithContext(ctx, "POST", control.Address+"/exposure", bytes.NewReader(encoded))
	if e != nil {
		return expose.Public{}, proxyFailure("proxy_unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+control.Token)
	client := &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, e := client.Do(request)
	if e != nil {
		return expose.Public{}, proxyFailure("proxy_pending")
	}
	defer response.Body.Close()
	var result exposureRPCResult
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil {
		return expose.Public{}, proxyFailure("proxy_unavailable")
	}
	if result.Error != nil {
		result.Error.ExitCode = result.ExitCode
		if result.Error.ExitCode == 0 {
			result.Error.ExitCode = 3
		}
	}
	return result.Item, result.Error
}
