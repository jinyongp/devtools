//go:build !windows

package expose

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestLocalAPIConditionalWriteAndCredentialBoundary(t *testing.T) {
	const token = "LOCAL_API_PRIVATE_CANARY"
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "" || password != token {
			t.Error("missing private LocalAPI authentication")
		}
		if r.URL.Path != "/localapi/v0/serve-config" {
			t.Error(r.URL.Path)
		}
		if r.Method == "GET" {
			w.Header().Set("ETag", `"current"`)
			_, _ = w.Write([]byte(`{"FutureField":{"preserved":true}}`))
			return
		}
		if r.Header.Get("If-Match") != `"current"` {
			w.WriteHeader(412)
			return
		}
		var config map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&config) != nil || !strings.Contains(string(config["FutureField"]), "preserved") {
			t.Error("unselected configuration lost")
		}
		posts++
		w.WriteHeader(200)
	}))
	defer server.Close()
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	p := Tailscale{Run: func(_ context.Context, args []string) ([]byte, *protocol.Error) {
		if strings.Join(args, " ") != "debug local-creds" {
			t.Fatal(args)
		}
		return []byte("curl -u:" + token + " http://localhost:" + port + "/localapi/v0/status\n"), nil
	}}
	api, e := p.configAPI(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	raw, etag, e := api.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if e := changeServeHandler(raw, "device.tailnet.ts.net:8443", 8443, "http://127.0.0.1:34000", false); e != nil {
		t.Fatal(e)
	}
	if e := api.Write(context.Background(), raw, etag); e != nil {
		t.Fatal(e)
	}
	if e := api.Write(context.Background(), raw, `"stale"`); e == nil || e.Code != "tailscale_configuration_changed" {
		t.Fatal(e)
	}
	if posts != 1 {
		t.Fatal(posts)
	}
	data, _ := json.Marshal(raw)
	if strings.Contains(string(data), token) {
		t.Fatal("LocalAPI credential entered configuration")
	}
}
