package proxy

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
)

type publicIngress struct{ Authority, Scheme string }
type ingressKey struct{}
type exposureRuntime struct {
	mu       sync.Mutex
	stopMu   sync.Mutex
	record   expose.Record
	auth     string
	sessions expose.Sessions
	bridge   *http.Server
	provider expose.ProviderSession
	owner    *networkRuntime
}

func validateLANListener(l network.Listener, httpPort, httpsPort int) *protocol.Error {
	host := l.HTTPHost
	if httpsPort > 0 {
		host = l.HTTPSHost
	} else if httpPort == 0 {
		return expose.Failure("lan_listener_required")
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() {
		return expose.Failure("lan_listener_required")
	}
	return nil
}

func (r *networkRuntime) exposure(q exposureRPCRequest) (expose.Public, *protocol.Error) {
	r.exposureMu.Lock()
	defer r.exposureMu.Unlock()
	store := expose.Store{Data: r.manager.Data}
	record, e := store.Read(q.ID)
	if e != nil {
		return expose.Public{}, e
	}
	if record.Generation != q.Generation {
		return expose.Public{}, expose.Failure("request_conflict")
	}
	current := r.exposures[q.ID]
	if q.Action == "status" {
		if current != nil {
			current.mu.Lock()
			defer current.mu.Unlock()
			return current.record.Public, nil
		}
		if record.State == "active" || record.State == "starting" {
			record.State = "recovery_required"
			record.Reason = "daemon_restarted"
			if record.Auth {
				record.State = "auth_required"
				record.Reason = "authentication_lost"
			}
		}
		return record.Public, nil
	}
	if q.Action == "stop" {
		if current != nil {
			return current.stop("stop_requested")
		}
		if record.State == "stopped" {
			return record.Public, nil
		}
		// A missing provider session cannot be reconstructed from a PID. LAN
		// has no external side effects; providers reconcile their exact owner.
		if record.Provider != "lan" && record.ProviderOwner != "" {
			provider := r.manager.ExposureProviders[record.Provider]
			if provider == nil && record.Provider == "tailscale" {
				provider = expose.Tailscale{Data: r.manager.Data}
			}
			if provider == nil && record.Provider == "cloudflared" {
				provider = expose.Cloudflared{Data: r.manager.Data}
			}
			recovery, ok := provider.(interface {
				RecoverStop(context.Context, expose.Record) *protocol.Error
			})
			if !ok {
				return record.Public, expose.Failure("provider_recovery_required")
			}
			if e := recovery.RecoverStop(r.ctx, record); e != nil {
				return record.Public, e
			}
		}
		record.State = "stopped"
		record.Reason = "stop_requested"
		if e := store.Write(record); e != nil {
			return record.Public, e
		}
		return record.Public, nil
	}
	if q.Action != "start" {
		return expose.Public{}, expose.Failure("invalid_argument")
	}
	if current != nil {
		current.mu.Lock()
		defer current.mu.Unlock()
		return current.record.Public, nil
	}
	if record.State == "stopped" || record.State == "auth_required" {
		return record.Public, expose.Failure("auth_required")
	}
	if record.Auth {
		if e := expose.ValidateAuth(q.Auth, false); e != nil {
			return record.Public, e
		}
	} else if q.Auth != "" {
		return record.Public, expose.Failure("request_conflict")
	}
	x := &exposureRuntime{record: record, auth: q.Auth, owner: r}
	if !x.valid() {
		return record.Public, expose.Failure("exposure_identity_changed")
	}
	if record.Provider == "lan" {
		for _, other := range r.exposures {
			other.mu.Lock()
			conflict := other.record.State == "active" && other.record.Host == record.Host && other.record.Listener == record.Listener
			other.mu.Unlock()
			if conflict {
				return record.Public, expose.Failure("host_conflict")
			}
		}
	} else {
		provider := r.manager.ExposureProviders[record.Provider]
		if provider == nil && record.Provider == "cloudflared" {
			provider = expose.Cloudflared{Data: r.manager.Data}
		}
		if provider == nil && record.Provider == "tailscale" {
			provider = expose.Tailscale{Data: r.manager.Data}
		}
		if provider == nil {
			return record.Public, expose.Failure("provider_not_installed")
		}
		socket, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", itoa(record.OriginPort)))
		if err != nil {
			return record.Public, expose.Failure("origin_bind_failed")
		}
		x.bridge = &http.Server{Handler: http.HandlerFunc(x.serve), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
		go func() { _ = x.bridge.Serve(socket) }()
		session, e := provider.Start(r.ctx, record, "http://"+socket.Addr().String())
		if e != nil {
			x.bridge.Close()
			if existing, readErr := store.Read(record.ID); readErr == nil && existing.Generation == record.Generation {
				record.ProviderOwner = existing.ProviderOwner
				record.URL = existing.URL
			}
			record.State = "recovery_required"
			record.Reason = e.Code
			_ = store.Write(record)
			return record.Public, e
		}
		x.provider = session
		u, err := url.Parse(session.URL())
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
			_ = session.Stop(context.Background())
			x.bridge.Close()
			return record.Public, expose.Failure("invalid_provider_url")
		}
		x.record.URL = strings.TrimSuffix(u.String(), "/")
		x.record.ProviderOwner = session.Owner()
	}
	x.record.Daemon = r.attemptID
	x.record.State = "active"
	x.record.Reason = ""
	if e := store.Write(x.record); e != nil {
		_ = x.sessions.Close(context.Background())
		if x.bridge != nil {
			x.bridge.Close()
		}
		if x.provider != nil {
			_ = x.provider.Stop(context.Background())
		}
		return record.Public, e
	}
	r.exposures[q.ID] = x
	return x.record.Public, nil
}
func itoa(number int) string { return strconv.Itoa(number) }

func (x *exposureRuntime) valid() bool {
	_, listener, digest, e := x.owner.manager.exposureIdentity(context.Background(), x.record.Route)
	if e != nil || digest != x.record.Identity || listener.ID != x.record.Pin.ListenerID || listener.Generation != x.record.Pin.ListenerGeneration {
		return false
	}
	x.owner.mu.Lock()
	group := x.owner.groups[listener.ID]
	running := group != nil && group.record.State == "running" && group.record.EndedAt == nil
	x.owner.mu.Unlock()
	if !running {
		return false
	}
	st, e := x.owner.manager.portStore().Read()
	if e != nil || st.Network == nil {
		return false
	}
	pin := false
	for _, p := range st.Network.Pins {
		pin = pin || p == x.record.Pin
	}
	if !pin {
		return false
	}
	if x.record.Reservation != "" {
		reservation := st.Reservation(x.record.Reservation)
		if reservation == nil || reservation.Port != x.record.OriginPort {
			return false
		}
	}
	return true
}
func (x *exposureRuntime) authorized(request *http.Request) bool {
	if !x.record.Auth {
		return true
	}
	user, password, ok := request.BasicAuth()
	if !ok {
		return false
	}
	expected := sha256.Sum256([]byte(x.auth))
	actual := sha256.Sum256([]byte(user + ":" + password))
	return subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}
func (x *exposureRuntime) serve(w http.ResponseWriter, request *http.Request) {
	x.mu.Lock()
	active := x.record.State == "active"
	record := x.record
	authorized := x.authorized(request)
	x.mu.Unlock()
	if !active || !x.valid() {
		http.Error(w, "exposure unavailable", 503)
		return
	}
	u, e := url.Parse(record.URL)
	if e != nil || !strings.EqualFold(request.Host, u.Host) {
		http.Error(w, "exposure authority mismatch", 403)
		return
	}
	if request.TLS != nil && !strings.EqualFold(request.TLS.ServerName, u.Hostname()) {
		http.Error(w, "exposure authority mismatch", 403)
		return
	}
	if !authorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="devtools", charset="UTF-8"`)
		http.Error(w, "authentication required", 401)
		return
	}
	writer, req, end, ok := x.sessions.Begin(w, request)
	if !ok {
		http.Error(w, "exposure unavailable", 503)
		return
	}
	defer end()
	req = req.WithContext(context.WithValue(req.Context(), ingressKey{}, publicIngress{Authority: u.Host, Scheme: u.Scheme}))
	h := NewHandler(x.owner.resolver)
	h.forward(writer, req, record.Pin.Port)
}

func (x *exposureRuntime) stop(reason string) (expose.Public, *protocol.Error) {
	x.stopMu.Lock()
	defer x.stopMu.Unlock()
	x.mu.Lock()
	if x.record.State == "stopped" {
		value := x.record.Public
		x.mu.Unlock()
		return value, nil
	}
	x.record.State = "stopping"
	x.record.Reason = reason
	record := x.record
	x.mu.Unlock()
	store := expose.Store{Data: x.owner.manager.Data}
	if e := store.Write(record); e != nil {
		return record.Public, e
	}
	if !x.sessions.Close(context.Background()) {
		return record.Public, expose.Failure("stream_recovery_required")
	}
	if x.bridge != nil {
		_ = x.bridge.Close()
	}
	if x.provider != nil {
		if e := x.provider.Stop(context.Background()); e != nil {
			x.mu.Lock()
			x.record.State = "recovery_required"
			x.record.Reason = e.Code
			record = x.record
			x.mu.Unlock()
			_ = store.Write(record)
			return record.Public, e
		}
	}
	x.mu.Lock()
	x.auth = ""
	x.record.State = "stopped"
	if reason != "stop_requested" {
		x.record.State = "recovery_required"
		if reason == "daemon_stopped" && x.record.Auth {
			x.record.State = "auth_required"
		}
	}
	x.record.Reason = reason
	record = x.record
	x.mu.Unlock()
	if e := store.Write(record); e != nil {
		return record.Public, e
	}
	return record.Public, nil
}
func (r *networkRuntime) watchExposures(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		r.exposureMu.Lock()
		entries := []*exposureRuntime{}
		for _, x := range r.exposures {
			entries = append(entries, x)
		}
		r.exposureMu.Unlock()
		for _, x := range entries {
			x.mu.Lock()
			active := x.record.State == "active"
			x.mu.Unlock()
			if !active {
				continue
			}
			reason := ""
			if !x.valid() {
				reason = "exposure_identity_changed"
			}
			if reason != "" {
				go func() { _, _ = x.stop(reason) }()
			} else if x.provider != nil {
				go func() {
					probe, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
					defer cancel()
					if !x.provider.Alive(probe) {
						_, _ = x.stop("provider_unavailable")
					}
				}()
			}
		}
	}
}

func (r *networkRuntime) lanExposure(host, listener string) *exposureRuntime {
	r.exposureMu.Lock()
	defer r.exposureMu.Unlock()
	for _, x := range r.exposures {
		x.mu.Lock()
		match := x.record.Provider == "lan" && x.record.State == "active" && x.record.Host == host && x.record.Listener == listener
		x.mu.Unlock()
		if match {
			return x
		}
	}
	return nil
}
