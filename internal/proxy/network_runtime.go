package proxy

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

const networkControlVersion = 1

type ListenerStatus struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Generation string     `json:"generation"`
	Running    bool       `json:"running"`
	State      string     `json:"state"`
	Port       int        `json:"port"`
	HTTPSPort  int        `json:"https_port"`
	URL        string     `json:"url"`
	StartedAt  *time.Time `json:"started_at"`
	Reason     string     `json:"reason"`
}
type listenerTarget struct {
	Definition network.Listener `json:"definition"`
	HTTPPort   int              `json:"http_port"`
	HTTPSPort  int              `json:"https_port"`
	AttemptID  string           `json:"attempt_id"`
	Create     bool             `json:"create"`
}
type networkControlRequest struct {
	Action string         `json:"action"`
	Target listenerTarget `json:"target"`
}
type networkControlResult struct {
	Item     ListenerStatus  `json:"item"`
	Error    *protocol.Error `json:"error,omitempty"`
	ExitCode int             `json:"error_exit_code,omitempty"`
}
type listenerGroup struct {
	target      listenerTarget
	record      Record
	servers     []*http.Server
	connections *connectionRegistry
	transport   *http.Transport
}

func (g *listenerGroup) close() {
	for _, server := range g.servers {
		_ = server.Close()
	}
	g.connections.closeAll()
	g.transport.CloseIdleConnections()
}

type networkRuntime struct {
	ctx        context.Context
	attemptID  string
	exposureMu sync.Mutex
	exposures  map[string]*exposureRuntime
	manager    Manager
	groups     map[string]*listenerGroup
	mu         sync.Mutex
	resolver   Resolver
}

func listenerRecord(id string) string { return "listeners/" + id + ".json" }
func targetStatus(t listenerTarget, record Record) ListenerStatus {
	url := ""
	if t.HTTPPort > 0 || t.HTTPSPort > 0 {
		url, _ = network.RouteURL("localhost", t.HTTPPort, t.HTTPSPort)
	}
	state := record.State
	if state == "" {
		state = "stopped"
	}
	return ListenerStatus{ID: t.Definition.ID, Name: t.Definition.Name, Generation: record.AttemptID, Running: state == "running" && record.EndedAt == nil, State: state, Port: t.HTTPPort, HTTPSPort: t.HTTPSPort, URL: url, StartedAt: record.StartedAt, Reason: record.Reason}
}
func (r *networkRuntime) start(ctx context.Context, t listenerTarget) (ListenerStatus, *protocol.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, e := r.manager.portStore().Read()
	if e != nil {
		return ListenerStatus{}, e
	}
	definition := st.Listener(t.Definition.Name)
	if definition == nil || *definition != t.Definition {
		return ListenerStatus{}, proxyFailure("request_conflict")
	}
	httpPort, httpsPort, e := st.ListenerPorts(definition.Name)
	if e != nil || httpPort != t.HTTPPort || httpsPort != t.HTTPSPort {
		return ListenerStatus{}, proxyFailure("request_conflict")
	}
	if group := r.groups[definition.ID]; group != nil {
		if group.record.AttemptID != t.AttemptID {
			return ListenerStatus{}, proxyFailure("request_conflict")
		}
		return targetStatus(t, group.record), nil
	}
	if !t.Create {
		return ListenerStatus{}, proxyFailure("proxy_start_failed")
	}
	var previous Record
	if e := tasks.ReadPrivate(r.manager.path(listenerRecord(definition.ID)), &previous); e == nil && previous.AttemptID == t.AttemptID && previous.EndedAt != nil {
		return ListenerStatus{}, proxyFailure("proxy_start_failed")
	}
	var authority *ca.Authority
	if httpsPort > 0 {
		var err *protocol.Error
		authority, _, err = (ca.Store{Data: r.manager.Data}).Init(ctx)
		if err != nil {
			return ListenerStatus{}, err
		}
	}
	transport := loopbackTransport()
	handler := &Handler{Routes: r.resolver, Transport: transport}
	handler.Listener = definition.Name
	handler.HTTPSPort = httpsPort
	handler.Guard = func(request *http.Request, _ Item) bool {
		host, _, e := net.SplitHostPort(request.RemoteAddr)
		return e == nil && net.ParseIP(host).IsLoopback()
	}
	frontend := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		host, ok := requestHost(request.Host)
		if ok {
			if sharing := r.lanExposure(host, definition.Name); sharing != nil {
				sharing.serve(w, request)
				return
			}
		}
		handler.ServeHTTP(w, request)
	})
	connections := &connectionRegistry{}
	group := &listenerGroup{target: t, connections: connections, transport: transport, record: Record{CreatedAt: time.Now().UTC(), AttemptID: t.AttemptID, State: "starting"}}
	var sockets []net.Listener
	rollback := func() {
		for _, s := range sockets {
			_ = s.Close()
		}
		group.close()
	}
	open := func(host string, port int, secure bool) *protocol.Error {
		if port == 0 {
			return nil
		}
		var opened []net.Listener
		var err error
		if !secure && definition.LegacyLoopback {
			opened, err = ListenLoopback(port)
		} else {
			var socket net.Listener
			socket, err = ports.ListenTCP(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
			if err == nil {
				opened = []net.Listener{socket}
			}
		}
		if err != nil {
			return listenerBindFailure(err)
		}

		for _, socket := range opened {
			sockets = append(sockets, socket)
			server := &http.Server{Handler: frontend, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
			if secure {
				var mu sync.Mutex
				certificates := map[string]tls.Certificate{}
				server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
					items, e := r.resolver.List(hello.Context(), "")
					if e != nil {
						return nil, e
					}
					valid := false
					if sharing := r.lanExposure(hello.ServerName, definition.Name); sharing != nil {
						valid = sharing.valid()
					}
					for _, item := range items {
						if item.Host != nil && *item.Host == hello.ServerName && item.Listener == definition.Name && item.Status == StatusReady {
							valid = true
						}
					}
					if !valid {
						return nil, errors.New("inactive TLS route")
					}
					mu.Lock()
					defer mu.Unlock()
					cert, ok := certificates[hello.ServerName]
					if !ok || cert.Leaf == nil || time.Until(cert.Leaf.NotAfter) < 24*time.Hour {
						var e *protocol.Error
						cert, e = authority.Leaf(hello.ServerName, time.Now().UTC())
						if e != nil {
							return nil, e
						}
						certificates[hello.ServerName] = cert
					}
					return &cert, nil
				}}
			}
			group.servers = append(group.servers, server)
		}
		return nil
	}
	if e := open(definition.HTTPHost, httpPort, false); e != nil {
		rollback()
		return ListenerStatus{}, e
	}
	if e := open(definition.HTTPSHost, httpsPort, true); e != nil {
		rollback()
		return ListenerStatus{}, e
	}
	now := time.Now().UTC()
	group.record.State = "running"
	group.record.StartedAt = &now
	if e := r.manager.write(listenerRecord(definition.ID), group.record); e != nil {
		rollback()
		return ListenerStatus{}, proxyStorageError()
	}
	r.groups[definition.ID] = group
	r.manager.listenerEvent(*definition, group.record)
	for i, server := range group.servers {
		socket := sockets[i]
		go func() {
			var err error
			if server.TLSConfig != nil {
				err = server.ServeTLS(trackedListener{Listener: socket, registry: connections}, "", "")
			} else {
				err = server.Serve(trackedListener{Listener: socket, registry: connections})
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
				r.stop(t, "listener_failed")
			}
		}()
	}
	return targetStatus(t, group.record), nil
}
func (r *networkRuntime) stop(t listenerTarget, reason string) (ListenerStatus, *protocol.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, e := r.manager.portStore().Read()
	if e != nil {
		return ListenerStatus{}, e
	}
	if reason == "stop_requested" && st.ListenerPinned(t.Definition.ID) {
		return ListenerStatus{}, proxyFailure("pinned")
	}
	group := r.groups[t.Definition.ID]
	if group == nil {
		var record Record
		_ = tasks.ReadPrivate(r.manager.path(listenerRecord(t.Definition.ID)), &record)
		if record.AttemptID != "" && record.AttemptID != t.AttemptID {
			return ListenerStatus{}, proxyFailure("request_conflict")
		}
		return targetStatus(t, record), nil
	}
	if group.record.AttemptID != t.AttemptID {
		return ListenerStatus{}, proxyFailure("request_conflict")
	}
	group.close()
	now := time.Now().UTC()
	group.record.State = "stopped"
	group.record.EndedAt = &now
	group.record.Reason = reason
	if e := r.manager.write(listenerRecord(t.Definition.ID), group.record); e != nil {
		return ListenerStatus{}, proxyStorageError()
	}
	delete(r.groups, t.Definition.ID)
	r.manager.listenerEvent(t.Definition, group.record)
	return targetStatus(t, group.record), nil
}

func (m Manager) serveNetwork(ctx context.Context, attemptID string) error {
	release, e := tasks.Lock(ctx, m.path("lease.lock"))
	if e != nil {
		return e
	}
	defer release()
	record, e := m.readRecord()
	if e != nil || record.AttemptID != attemptID || record.State != "starting" || record.EndedAt != nil {
		return errors.New("invalid network daemon attempt")
	}
	runtime := &networkRuntime{manager: m, groups: map[string]*listenerGroup{}, resolver: NewResolver(m.portStore()), exposures: map[string]*exposureRuntime{}, attemptID: attemptID}
	listen := m.ControlListen
	if listen == nil {
		listen = func() (net.Listener, error) { return ports.ListenTCP(ctx, "tcp4", "127.0.0.1:0") }
	}
	socket, e := listen()
	if e != nil {
		m.failAttempt(attemptID, "control_bind_failed")
		return e
	}
	defer socket.Close()
	secret := make([]byte, 32)
	if _, e := rand.Read(secret); e != nil {
		return e
	}
	control := proxyControl{Address: "http://" + socket.Addr().String(), Token: hex.EncodeToString(secret), Protocol: networkControlVersion}
	if e := m.write("control.json", control); e != nil {
		return e
	}
	serveCtx, cancel := context.WithCancel(ctx)
	runtime.ctx = serveCtx
	go runtime.watchExposures(serveCtx)
	defer cancel()
	server := &http.Server{ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 60 * time.Second, MaxHeaderBytes: 8192}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if request.Method != "POST" || request.Host != strings.TrimPrefix(control.Address, "http://") || request.Header.Get("Origin") != "" || request.Header.Get("Authorization") != "Bearer "+control.Token {
			http.Error(w, "Forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/status" || request.URL.Path == "/stop" {
			current, e := m.readRecord()
			if e != nil {
				http.Error(w, "Unavailable", 503)
				return
			}
			_ = json.NewEncoder(w).Encode(current)
			if request.URL.Path == "/stop" {
				cancel()
			}
			return
		}
		if request.URL.Path == "/exposure" {
			var q exposureRPCRequest
			decoder := json.NewDecoder(io.LimitReader(request.Body, 65536))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&q) != nil || decoder.Decode(new(any)) != io.EOF {
				http.Error(w, "Invalid request", 400)
				return
			}
			item, e := runtime.exposure(q)
			result := exposureRPCResult{Item: item, Error: e}
			if e != nil {
				result.ExitCode = e.ExitCode
			}
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		if request.URL.Path != "/network" {
			http.NotFound(w, request)
			return
		}
		var q networkControlRequest
		decoder := json.NewDecoder(io.LimitReader(request.Body, 65536))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&q) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "Invalid request", 400)
			return
		}
		result := networkControlResult{}
		switch q.Action {
		case "start":
			result.Item, result.Error = runtime.start(request.Context(), q.Target)
		case "stop":
			result.Item, result.Error = runtime.stop(q.Target, "stop_requested")
		default:
			result.Error = proxyFailure("invalid_argument")
		}
		if result.Error != nil {
			result.ExitCode = result.Error.ExitCode
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.Serve(socket) }()
	defer server.Close()
	now := time.Now().UTC()
	_, changed, e := m.changeAttempt(ctx, attemptID, func(active *Record) bool {
		if active.State != "starting" || active.EndedAt != nil {
			return false
		}
		active.State = "running"
		active.StartedAt = &now
		return true
	})
	if e != nil || !changed {
		return errors.New("network daemon attempt changed")
	}
	select {
	case <-serveCtx.Done():
	case <-errorsCh:
	}
	runtime.exposureMu.Lock()
	for _, x := range runtime.exposures {
		_, _ = x.stop("daemon_stopped")
	}
	runtime.exposureMu.Unlock()
	runtime.mu.Lock()
	for _, group := range runtime.groups {
		group.close()
		now := time.Now().UTC()
		group.record.EndedAt = &now
		group.record.State = "interrupted"
		group.record.Reason = "daemon_stopped"
		_ = m.write(listenerRecord(group.target.Definition.ID), group.record)
		m.listenerEvent(group.target.Definition, group.record)
	}
	runtime.mu.Unlock()
	now = time.Now().UTC()
	_, _, e = m.changeAttempt(context.Background(), attemptID, func(active *Record) bool {
		active.State = "stopped"
		active.EndedAt = &now
		active.Reason = "stop_requested"
		return true
	})
	return e
}

func listenerBindFailure(err error) *protocol.Error {
	if ports.BindPermissionDenied(err) {
		return proxyFailure("permission_required")
	}
	if ports.BindAddressInUse(err) {
		return proxyFailure("port_in_use")
	}
	return proxyFailure("proxy_start_failed")
}
