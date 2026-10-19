package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestDashboardProtectedValuesAndVaultActionsStayPrivate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := privateTempDir(t)
	directory := filepath.Join(root, "vault")
	id, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-dashboard-vault-passphrase"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: directory}).Initialize(ctx, id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	if err = (vault.FileStore{Root: root, VaultID: id, CatalogKey: master}).Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- vault.ServeKeySession(ctx, directory) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("dashboard test session did not stop")
		}
	})
	var client *vault.SessionClient
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err = vault.OpenSessionClient(directory)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	server := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(root, "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	call := func(method, path, body string, authenticated, origin bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, server.registry.Address+path, strings.NewReader(body))
		if authenticated {
			request.Header.Set("Authorization", "Bearer session")
		}
		if origin {
			request.Header.Set("Origin", server.registry.Address)
		}
		if method == "POST" {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		for _, canary := range []string{"synthetic-dashboard-vault-passphrase", "synthetic-dashboard-secret"} {
			if strings.Contains(response.Body.String(), canary) {
				t.Fatal("private input leaked in dashboard response")
			}
		}
		return response
	}
	if response := call("GET", "/api/vault", "", false, false); response.Code != 401 {
		t.Fatal("unauthenticated vault status", response.Code)
	}
	if response := call("POST", "/api/actions", `{"domain":"vault","vault":{"action":"lock"}}`, true, false); response.Code != 403 {
		t.Fatal("vault action accepted without same origin", response.Code)
	}
	response := call("GET", "/api/values?profile=app", "", true, false)
	var view values.ManagedView
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil {
		t.Fatal("metadata view", response.Code)
	}
	secret := "synthetic-dashboard-secret"
	body, _ := json.Marshal(actionRequest{Domain: "values", Profile: "app", Change: &values.Change{Action: "secret.set", Key: "VITE_TOKEN", Value: &secret, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000161"}})
	if response = call("POST", "/api/actions", string(body), true, true); response.Code != 200 {
		t.Fatal("protected dashboard write", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "frontend_public_prefix") {
		t.Fatal("dashboard omitted frontend name warning")
	}
	if response = call("POST", "/api/actions", `{"domain":"vault","vault":{"action":"lock"}}`, true, true); response.Code != 200 {
		t.Fatal("dashboard lock", response.Code)
	}
	before, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/vault", "/api/values?profile=app"} {
		if response = call("GET", path, "", true, false); response.Code != 200 {
			t.Fatal("locked metadata", response.Code)
		}
	}
	after, err := client.Status(ctx)
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if err != nil || !bytes.Equal(a, b) || after.State != vault.LockedManual {
		t.Fatal("metadata opened/touched locked session", err)
	}
	if response = call("POST", "/api/actions", string(body), true, true); response.Code == 200 || !strings.Contains(response.Body.String(), "vault_locked") {
		t.Fatal("locked write accepted", response.Code)
	}
	for _, input := range []string{`{"domain":"vault","vault":{"action":"unlock","passphrase":"wrong"}}`, `{"domain":"vault","vault":{"action":"lock","passphrase":"forbidden"}}`} {
		if response = call("POST", "/api/actions", input, true, true); response.Code == 200 {
			t.Fatal("invalid unlock inputs accepted")
		}
	}
	response = call("POST", "/api/actions", `{"domain":"vault","vault":{"action":"unlock","passphrase":"synthetic-dashboard-vault-passphrase"}}`, true, true)
	if response.Code != 200 {
		t.Fatal("dashboard explicit unlock", response.Code, response.Body)
	}
	if response = call("GET", "/api/vault?passphrase=synthetic-dashboard-vault-passphrase", "", true, false); response.Code != 400 {
		t.Fatal("vault query inputs accepted", response.Code)
	}
}
