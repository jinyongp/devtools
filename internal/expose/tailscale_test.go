package expose

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type fakeTailscale struct {
	status         string
	config         map[string]any
	mutations      [][]string
	failStop       bool
	beforeMutation func()
}

func newFakeTailscale() *fakeTailscale {
	return &fakeTailscale{status: `{"Version":"1.104.1","BackendState":"Running","Self":{"DNSName":"device.tailnet.ts.net."},"CertDomains":["device.tailnet.ts.net"],"UnknownFutureField":true}`, config: map[string]any{"TCP": map[string]any{}, "Web": map[string]any{}, "FutureField": "accepted"}}
}

func (f *fakeTailscale) Read(context.Context) (map[string]json.RawMessage, string, *protocol.Error) {
	data, _ := json.Marshal(f.config)
	var config map[string]json.RawMessage
	_ = json.Unmarshal(data, &config)
	return config, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
func (f *fakeTailscale) Write(ctx context.Context, config map[string]json.RawMessage, etag string) *protocol.Error {
	f.mutations = append(f.mutations, []string{"If-Match", etag})
	if f.beforeMutation != nil {
		f.beforeMutation()
	}
	_, current, _ := f.Read(ctx)
	if current != etag {
		return Failure("tailscale_configuration_changed")
	}
	if f.failStop {
		return Failure("tailscale_command_failed")
	}
	data, _ := json.Marshal(config)
	returnErr := json.Unmarshal(data, &f.config)
	if returnErr != nil {
		return Failure("tailscale_status_invalid")
	}
	return nil
}
func (f *fakeTailscale) run(_ context.Context, args []string) ([]byte, *protocol.Error) {
	if strings.Join(args, " ") == "status --json" {
		return []byte(f.status), nil
	}
	if strings.Join(args, " ") == "serve status --json" {
		data, _ := json.Marshal(f.config)
		return data, nil
	}
	return nil, Failure("unexpected_command")
}

func tailscaleRecord() Record {
	id, gen, route := tasks.ID(), tasks.ID(), network.RouteRef{Scope: "global", RouteID: tasks.ID()}
	return Record{Public: Public{ID: id, Generation: gen, Route: route, Provider: "tailscale", State: "starting", ServePort: 8443, OriginPort: 34000}, Pin: network.Pin{ID: id, Route: route, ListenerID: tasks.ID(), ListenerGeneration: tasks.ID(), NetworkDigest: strings.Repeat("0", 64), Port: 31000, Provider: "tailscale"}}
}
func TestTailscaleSelectedHandlerOwnershipAndRecovery(t *testing.T) {
	f := newFakeTailscale()
	f.config["TCP"].(map[string]any)["443"] = map[string]any{"HTTPS": true}
	f.config["Web"].(map[string]any)["device.tailnet.ts.net:443"] = map[string]any{"Handlers": map[string]any{"/": map[string]any{"Text": "user-owned"}}}
	p := Tailscale{Data: t.TempDir(), Run: f.run, API: f}
	record := tailscaleRecord()
	origin := "http://127.0.0.1:34000"
	f.beforeMutation = func() {
		stored, e := (Store{Data: p.Data}).Read(record.ID)
		if e != nil || stored.ProviderOwner == "" {
			t.Fatal("provider modified before owner journal", e)
		}
	}
	s, e := p.Start(context.Background(), record, origin)
	if e != nil {
		t.Fatal(e)
	}
	if s.URL() != "https://device.tailnet.ts.net:8443" || !s.Alive(context.Background()) {
		t.Fatal("wrong public endpoint")
	}
	// A later sibling path belongs to the user and must survive root removal.
	handlers := f.config["Web"].(map[string]any)["device.tailnet.ts.net:8443"].(map[string]any)["Handlers"].(map[string]any)
	handlers["/user"] = map[string]any{"Text": "retained"}
	f.failStop = true
	if e := s.Stop(context.Background()); e == nil {
		t.Fatal("stop failure hidden")
	}
	f.failStop = false
	stored, e := (Store{Data: p.Data}).Read(record.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e := p.RecoverStop(context.Background(), stored); e != nil {
		t.Fatal(e)
	}
	if _, ok := handlers["/user"]; !ok {
		t.Fatal("sibling handler deleted")
	}
	if _, ok := f.config["Web"].(map[string]any)["device.tailnet.ts.net:443"]; !ok {
		t.Fatal("other HTTPS port deleted")
	}
	for _, args := range f.mutations {
		if strings.Contains(strings.Join(args, " "), "reset") {
			t.Fatal("global reset used")
		}
	}
}
func TestTailscaleRefusesLoginHTTPSConflictAndOwnershipDrift(t *testing.T) {
	for _, tc := range []struct{ name, status, code string }{
		{"login", `{"Version":"1.104.1","BackendState":"NeedsLogin"}`, "tailscale_not_logged_in"},
		{"https", `{"Version":"1.104.1","BackendState":"Running","Self":{"DNSName":"device.tailnet.ts.net."}}`, "tailscale_https_required"},
		{"version", `{"Version":"1.40.0","BackendState":"Running"}`, "tailscale_version_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTailscale()
			f.status = tc.status
			if _, e := (Tailscale{Run: f.run, API: f}).Start(context.Background(), tailscaleRecord(), "http://127.0.0.1:34000"); e == nil || e.Code != tc.code {
				t.Fatal(e)
			}
			if len(f.mutations) != 0 {
				t.Fatal("precondition failure modified provider")
			}
		})
	}
	f := newFakeTailscale()
	f.config["TCP"].(map[string]any)["8443"] = map[string]any{"TCPForward": "127.0.0.1:9000"}
	p := Tailscale{Run: f.run, API: f}
	if _, e := p.Start(context.Background(), tailscaleRecord(), "http://127.0.0.1:34000"); e == nil || e.Code != "tailscale_port_conflict" {
		t.Fatal(e)
	}
	delete(f.config["TCP"].(map[string]any), "8443")
	s, e := p.Start(context.Background(), tailscaleRecord(), "http://127.0.0.1:34000")
	if e != nil {
		t.Fatal(e)
	}
	f.config["Web"].(map[string]any)["device.tailnet.ts.net:8443"].(map[string]any)["Handlers"].(map[string]any)["/"] = map[string]any{"Proxy": "http://127.0.0.1:9000"}
	before := len(f.mutations)
	if e := s.Stop(context.Background()); e == nil || e.Code != "tailscale_ownership_changed" {
		t.Fatal(e)
	}
	if len(f.mutations) != before {
		t.Fatal("ownership drift caused mutation")
	}
}

func TestTailscaleConditionalWritePreservesConcurrentUserHandler(t *testing.T) {
	ctx := context.Background()
	for _, stop := range []bool{false, true} {
		f := newFakeTailscale()
		p := Tailscale{Run: f.run, API: f}
		var session ProviderSession
		if stop {
			var e *protocol.Error
			session, e = p.Start(ctx, tailscaleRecord(), "http://127.0.0.1:34000")
			if e != nil {
				t.Fatal(e)
			}
		}
		f.beforeMutation = func() {
			f.config["TCP"].(map[string]any)["8443"] = map[string]any{"HTTPS": true}
			f.config["Web"].(map[string]any)["device.tailnet.ts.net:8443"] = map[string]any{"Handlers": map[string]any{"/": map[string]any{"Text": "concurrent-user"}}}
		}
		var e *protocol.Error
		if stop {
			e = session.Stop(ctx)
		} else {
			_, e = p.Start(ctx, tailscaleRecord(), "http://127.0.0.1:34000")
		}
		if e == nil || e.Code != "tailscale_configuration_changed" {
			t.Fatal(stop, e)
		}
		data, _ := json.Marshal(f.config)
		if !strings.Contains(string(data), "concurrent-user") {
			t.Fatal("concurrent handler changed")
		}
	}
}
