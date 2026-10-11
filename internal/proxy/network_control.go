package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (m Manager) NetworkEnabled() (bool, *protocol.Error) {
	st, e := m.portStore().Read()
	return e == nil && st.Network != nil && len(st.Network.Listeners) > 0, e
}
func (m Manager) networkCapable() bool {
	var c proxyControl
	return tasks.ReadPrivate(m.path("control.json"), &c) == nil && c.Protocol == networkControlVersion
}

func (m Manager) NetworkStatus(ctx context.Context, name string) (map[string]any, *protocol.Error) {
	st, e := m.portStore().Read()
	if e != nil {
		return nil, e
	}
	definition := st.Listener(name)
	if definition == nil {
		return nil, proxyFailure("listener_not_found")
	}
	httpPort, httpsPort, e := st.ListenerPorts(name)
	if e != nil {
		return nil, e
	}
	target := listenerTarget{Definition: *definition, HTTPPort: httpPort, HTTPSPort: httpsPort}
	var record Record
	readErr := tasks.ReadPrivate(m.path(listenerRecord(definition.ID)), &record)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, proxyStorageError()
	}
	daemon, e := m.Status(ctx)
	if e != nil {
		return nil, e
	}
	if !daemon.Running && record.State == "running" {
		record.State = "interrupted"
		record.Reason = "supervisor_lost"
	}
	item := targetStatus(target, record)
	encoded, _ := json.Marshal(item)
	var result map[string]any
	_ = json.Unmarshal(encoded, &result)
	result["daemon_running"] = daemon.Running
	return result, nil
}

func (m Manager) Configure(ctx context.Context, name, httpAddress, httpsAddress string, disableHTTPS bool, id string) (any, *protocol.Error) {
	name = network.ListenerName(name)
	input := struct {
		Action, Name, HTTP, HTTPS string
		DisableHTTPS              bool
	}{"configure", name, httpAddress, httpsAddress, disableHTTPS}
	return (netops.Store{Data: m.Data}).Apply(ctx, id, input, []string{"proxy-daemon", "listener/" + name}, func(j *netops.Journal) (any, *protocol.Error) {
		st, e := m.portStore().Read()
		if e != nil {
			return nil, e
		}
		var identity []string
		if present, e := j.Get("identity", &identity); e != nil {
			return nil, e
		} else if present && len(identity) != 2 {
			return nil, proxyStorageError()
		}
		if len(identity) == 2 {
			if current := st.Listener(name); current != nil && current.ID == identity[0] && current.Generation == identity[1] {
				return map[string]any{"item": *current, "changed": true}, nil
			}
		}
		daemon, e := m.Status(ctx)
		if e != nil {
			return nil, e
		}
		if daemon.Running && !m.networkCapable() {
			return nil, proxyFailure("old_daemon_version")
		}
		if definition := st.Listener(name); definition != nil {
			status, e := m.NetworkStatus(ctx, name)
			if e != nil {
				return nil, e
			}
			if status["running"] == true {
				return nil, proxyFailure("listener_running")
			}
			var expected network.Listener
			present, e := j.Get("previous", &expected)
			if e != nil {
				return nil, e
			}
			if present && expected != *definition {
				return nil, proxyFailure("request_conflict")
			}
			if !present {
				if e := j.Set("previous", *definition); e != nil {
					return nil, e
				}
			}
		}
		if len(identity) == 0 {
			id := tasks.ID()
			if current := st.Listener(name); current != nil {
				id = current.ID
			}
			identity = []string{id, tasks.ID()}
			if e := j.Set("identity", identity); e != nil {
				return nil, e
			}
		}
		configured, e := m.portStore().ConfigureListener(ctx, name, httpAddress, httpsAddress, disableHTTPS, identity...)
		if e != nil {
			return nil, e
		}
		return map[string]any{"item": configured, "changed": true}, nil
	})
}

func (m Manager) ensureNetworkDefault(ctx context.Context) *protocol.Error {
	st, e := m.portStore().Read()
	if e != nil {
		return e
	}
	if st.Listener(network.DefaultListener) != nil {
		return nil
	}
	daemon, e := m.Status(ctx)
	if e != nil {
		return e
	}
	if daemon.Running {
		return proxyFailure("old_daemon_version")
	}
	_, e = m.portStore().ConfigureListener(ctx, network.DefaultListener, "", "", false)
	return e
}

func (m Manager) bootNetwork(ctx context.Context, j *netops.Journal) *protocol.Error {
	status, e := m.Status(ctx)
	if e != nil {
		return e
	}
	if status.Running {
		if !m.networkCapable() {
			return proxyFailure("old_daemon_version")
		}
		return nil
	}
	if status.State == "unknown" {
		return proxyFailure("proxy_unavailable")
	}
	var bootID string
	present, e := j.Get("daemon_attempt", &bootID)
	if e != nil {
		return e
	}
	if !present {
		bootID = tasks.ID()
		if e := j.Set("daemon_attempt", bootID); e != nil {
			return e
		}
	}
	unlock, err := tasks.Lock(ctx, m.path("transition.lock"))
	if err != nil {
		return proxyStorageError()
	}
	record, readErr := m.readRecord()
	if readErr == nil && record.AttemptID == bootID && record.EndedAt == nil {
		unlock()
	} else {
		if !m.leaseFree(ctx) {
			unlock()
			return proxyFailure("proxy_pending")
		}
		record = Record{CreatedAt: time.Now().UTC(), AttemptID: bootID, State: "starting"}
		if e := m.write("record.json", record); e != nil {
			unlock()
			return proxyStorageError()
		}
		unlock()
		spawn := m.Spawn
		if spawn == nil {
			spawn = spawnProxy
			if m.IsolatedRoot != "" {
				spawn = func(data, id string) error { return spawnProxyWithRoot(data, id, m.IsolatedRoot) }
			}
		}
		if e := spawn(m.Data, bootID); e != nil {
			m.failAttempt(bootID, "supervisor_start_failed")
			return proxyFailure("proxy_start_failed")
		}
	}
	wait, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, e := m.Status(wait)
		if e == nil && status.Running && m.networkCapable() {
			return nil
		}
		select {
		case <-wait.Done():
			return proxyFailure("proxy_pending")
		case <-ticker.C:
		}
	}
}

func (m Manager) NetworkApply(ctx context.Context, action, name string, all bool, id string, requested ...int) (any, *protocol.Error) {
	if action != "start" && action != "stop" && action != "restart" {
		return nil, proxyFailure("invalid_argument")
	}
	if all && name != "" {
		return nil, proxyFailure("invalid_argument")
	}
	name = network.ListenerName(name)
	port := 0
	if len(requested) > 0 {
		port = requested[0]
	}
	if port != 0 && (port < 1 || port > 65535 || all || action != "start") {
		return nil, proxyFailure("invalid_argument")
	}
	input := struct {
		Action, Listener string
		All              bool
		Port             int
	}{action, name, all, port}
	return (netops.Store{Data: m.Data}).Apply(ctx, id, input, []string{"proxy-daemon"}, func(j *netops.Journal) (any, *protocol.Error) {
		// A restart can replace the legacy daemon once. Its child receipt anchors
		// retries to the original process before the registry gains listeners.
		if action == "restart" {
			var stopID string
			present, e := j.Get("legacy_stop", &stopID)
			if e != nil {
				return nil, e
			}
			daemon, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			if !present && daemon.Running && !m.networkCapable() {
				if all || name == network.DefaultListener {
					stopID = tasks.ID()
					if e := j.Set("legacy_stop", stopID); e != nil {
						return nil, e
					}
				}
			}
			if stopID != "" {
				if _, e := m.Apply(ctx, Request{Action: "stop", RequestID: stopID}); e != nil {
					return nil, e
				}
			}
		}
		st, e := m.portStore().Read()
		if e != nil {
			return nil, e
		}
		if st.Network == nil || len(st.Network.Listeners) == 0 || !all && name == network.DefaultListener && st.Listener(name) == nil {
			if e := m.ensureNetworkDefault(ctx); e != nil {
				return nil, e
			}
			st, e = m.portStore().Read()
			if e != nil {
				return nil, e
			}
		}
		if port != 0 {
			definition := st.Listener(name)
			if definition == nil {
				return nil, proxyFailure("listener_not_found")
			}
			currentPort, _, e := st.ListenerPorts(name)
			if e != nil {
				return nil, e
			}
			if currentPort != port {
				status, e := m.NetworkStatus(ctx, name)
				if e != nil {
					return nil, e
				}
				if status["running"] == true {
					return nil, proxyFailure("proxy_conflict")
				}
				var identity []string
				present, e := j.Get("port_identity", &identity)
				if e != nil {
					return nil, e
				}
				if !present {
					identity = []string{definition.ID, tasks.ID(), definition.Generation}
					if e := j.Set("port_identity", identity); e != nil {
						return nil, e
					}
				}
				if len(identity) != 3 || definition.Generation != identity[2] {
					return nil, proxyFailure("request_conflict")
				}
				host := definition.HTTPHost
				if host == "" {
					host = "127.0.0.1"
				}
				if _, e := m.portStore().ConfigureListener(ctx, name, net.JoinHostPort(host, strconv.Itoa(port)), "", false, identity[:2]...); e != nil {
					return nil, e
				}
				st, e = m.portStore().Read()
				if e != nil {
					return nil, e
				}
			}
		}
		var targets []listenerTarget
		present, e := j.Get("targets", &targets)
		if e != nil {
			return nil, e
		}
		if !present {
			for _, definition := range st.Network.Listeners {
				if !all && definition.Name != name {
					continue
				}
				h, tls, e := st.ListenerPorts(definition.Name)
				if e != nil {
					return nil, e
				}
				var record Record
				_ = tasks.ReadPrivate(m.path(listenerRecord(definition.ID)), &record)
				daemon, e := m.Status(ctx)
				if e != nil {
					return nil, e
				}
				create := record.State != "running" || record.EndedAt != nil || !daemon.Running
				attempt := record.AttemptID
				if create && action == "start" {
					attempt = tasks.ID()
				}
				targets = append(targets, listenerTarget{Definition: definition, HTTPPort: h, HTTPSPort: tls, AttemptID: attempt, Create: create})
			}
			if len(targets) == 0 {
				return nil, proxyFailure("listener_not_found")
			}
			if e := j.Set("targets", targets); e != nil {
				return nil, e
			}
		}
		for _, target := range targets {
			definition := st.Listener(target.Definition.Name)
			if definition == nil || *definition != target.Definition {
				return nil, proxyFailure("request_conflict")
			}
			if (action == "stop" || action == "restart") && st.ListenerPinned(definition.ID) {
				return nil, proxyFailure("pinned")
			}
		}
		if action != "stop" {
			if e := m.bootNetwork(ctx, j); e != nil {
				return nil, e
			}
		}
		results := []ListenerStatus{}
		changed := false
		for index, target := range targets {
			key := "target-" + strconv.Itoa(index)
			var saved ListenerStatus
			if present, e := j.Get(key, &saved); e != nil {
				return nil, e
			} else if present {
				results = append(results, saved)
				changed = changed || action == "restart" || action == "start" && target.Create || action == "stop" && !target.Create
				continue
			}
			daemon, e := m.Status(ctx)
			if e != nil {
				return nil, e
			}
			if action == "restart" {
				var replacement listenerTarget
				present, e := j.Get(key+"-replacement", &replacement)
				if e != nil {
					return nil, e
				}
				if !present {
					if daemon.Running {
						if _, e := m.networkRPC(ctx, networkControlRequest{Action: "stop", Target: target}); e != nil {
							return nil, e
						}
					}
					replacement = target
					replacement.AttemptID = tasks.ID()
					replacement.Create = true
					if e := j.Set(key+"-replacement", replacement); e != nil {
						return nil, e
					}
				}
				target = replacement
			}
			if action == "stop" && !daemon.Running {
				saved = targetStatus(target, Record{State: "stopped", AttemptID: target.AttemptID})
			} else {
				rpcAction := action
				if action == "restart" {
					rpcAction = "start"
				}
				saved, e = m.networkRPC(ctx, networkControlRequest{Action: rpcAction, Target: target})
				if e != nil {
					return nil, e
				}
				changed = changed || action == "restart" || action == "start" && target.Create || action == "stop" && !target.Create
			}
			if e := j.Set(key, saved); e != nil {
				return nil, e
			}
			results = append(results, saved)
		}
		if all {
			return map[string]any{"items": results, "changed": changed}, nil
		}
		encoded, _ := json.Marshal(results[0])
		var item map[string]any
		_ = json.Unmarshal(encoded, &item)
		daemon, _ := m.Status(ctx)
		item["daemon_running"] = daemon.Running
		return map[string]any{"item": item, "changed": changed}, nil
	})
}

func (m Manager) networkRPC(ctx context.Context, q networkControlRequest) (ListenerStatus, *protocol.Error) {
	var control proxyControl
	if tasks.ReadPrivate(m.path("control.json"), &control) != nil || control.Protocol != networkControlVersion || len(control.Token) != 64 {
		return ListenerStatus{}, proxyFailure("old_daemon_version")
	}
	parsed, e := url.Parse(control.Address)
	if e != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ListenerStatus{}, proxyFailure("proxy_unavailable")
	}
	port, e := strconv.Atoi(parsed.Port())
	if e != nil || port < 1 || port > 65535 {
		return ListenerStatus{}, proxyFailure("proxy_unavailable")
	}
	encoded, _ := json.Marshal(q)
	request, e := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(control.Address, "/")+"/network", bytes.NewReader(encoded))
	if e != nil {
		return ListenerStatus{}, proxyFailure("proxy_unavailable")
	}
	request.Header.Set("Authorization", "Bearer "+control.Token)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, e := client.Do(request)
	if e != nil {
		return ListenerStatus{}, proxyFailure("proxy_pending")
	}
	defer response.Body.Close()
	var result networkControlResult
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil {
		return ListenerStatus{}, proxyFailure("proxy_unavailable")
	}
	if result.Error != nil {
		result.Error.ExitCode = result.ExitCode
		if result.Error.ExitCode == 0 {
			result.Error.ExitCode = 3
		}
	}
	return result.Item, result.Error
}

// NetworkStatuses is a read-only snapshot of configured listener diagnostics.
func (m Manager) NetworkStatuses(ctx context.Context) ([]map[string]any, *protocol.Error) {
	st, e := m.portStore().Read()
	if e != nil {
		return nil, e
	}
	items := []map[string]any{}
	if st.Network == nil {
		return items, nil
	}
	for _, definition := range st.Network.Listeners {
		item, e := m.NetworkStatus(ctx, definition.Name)
		if e != nil {
			return nil, e
		}
		items = append(items, item)
	}
	return items, nil
}

func PortStore(m Manager) ports.Store { return m.portStore() }
