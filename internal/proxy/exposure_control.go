package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
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
}
type exposureRPCRequest struct{ Action, ID, Generation, Auth string }
type exposureRPCResult struct {
	Item     expose.Public   `json:"item"`
	Error    *protocol.Error `json:"error,omitempty"`
	ExitCode int             `json:"error_exit_code,omitempty"`
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
	}{"expose-start", q.Route, q.Provider, q.Alias, commitment, q.NoAuth, q.ServePort}, []string{"exposure-route/" + q.Route.Key(), "exposures"}, func(j *netops.Journal) (any, *protocol.Error) {
		var record expose.Record
		present, e := j.Get("exposure", &record)
		if e != nil {
			return nil, e
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
				if _, e := m.RouteUp(ctx, q.Route, p, parent, hm, prepareID); e != nil {
					return nil, e
				}
			}
			item, listener, digest, e := m.exposureIdentity(ctx, q.Route)
			if e != nil {
				return nil, e
			}
			record = expose.Record{Public: expose.Public{ID: tasks.ID(), Route: q.Route, Provider: q.Provider, State: "preparing", Generation: tasks.ID(), Listener: listener.Name, Host: *item.Host, Auth: !q.NoAuth, ServePort: q.ServePort, CreatedAt: time.Now().UTC()}, Identity: digest}
			service := ""
			if item.Service != nil {
				service = *item.Service
			}
			record.Pin = network.Pin{ID: record.ID, Route: q.Route, ListenerID: listener.ID, ListenerGeneration: listener.Generation, NetworkDigest: digest, Port: *item.TargetPort, Provider: q.Provider, Service: service}
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
			return nil, e
		}
		return map[string]any{"item": active, "changed": true}, nil
	})
	if e != nil {
		return result, e
	}
	// A receipt is historical. Reading current metadata never restores lost auth.
	if value, ok := result.(map[string]any); ok {
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
		r.Reason = "daemon_unavailable"
		if r.Auth {
			r.State = "auth_required"
			r.Reason = "authentication_lost"
		}
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
	return (netops.Store{Data: m.Data}).Apply(ctx, requestID, struct{ Action, ID string }{"expose-stop", id}, []string{"exposures"}, func(j *netops.Journal) (any, *protocol.Error) {
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
		if r.State == "stopped" {
			return map[string]any{"item": r.Public, "changed": false}, nil
		}
		item, e := m.exposureRPC(ctx, exposureRPCRequest{Action: "stop", ID: r.ID, Generation: r.Generation})
		if e != nil {
			return nil, e
		}
		if e := m.portStore().Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			pins := []network.Pin{}
			for _, pin := range st.Network.Pins {
				if pin.ID != r.ID {
					pins = append(pins, pin)
				}
			}
			st.Network.Pins = pins
			reservations := []ports.Reservation{}
			for _, p := range st.Reservations {
				if p.Name != r.Reservation {
					reservations = append(reservations, p)
				}
			}
			st.Reservations = reservations
			return true, nil
		}); e != nil {
			return nil, e
		}
		return map[string]any{"item": item, "changed": true}, nil
	})
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
