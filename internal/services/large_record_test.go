package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestSupervisorRPCPreservesThousandDeliveredSlotReferences(t *testing.T) {
	data, err := filepath.EvalSymlinks(privateTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Data: data}
	record := Record{ID: tasks.ID(), Profile: "app", InputVersion: &values.InputVersion{Slots: []vault.SlotReference{}}}
	for n := range 1000 {
		record.InputVersion.Slots = append(record.InputVersion.Slots, vault.SlotReference{Name: fmt.Sprintf("KEY_%04d", n), Kind: vault.Public, ValueID: fmt.Sprintf("%032x", n+1), SlotID: fmt.Sprintf("%032x", n+1001), Version: fmt.Sprintf("%032x", n+2001)})
	}
	body, err := json.Marshal(record)
	if err != nil || len(body) <= 64<<10 {
		t.Fatal("fixture does not cross former control bound", err)
	}
	token := strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/status" || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer server.Close()
	if err := writePrivate(store.path(record.ID, "control.json"), control{ID: record.ID, Address: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	actual, err := store.rpc(context.Background(), record.ID, "status")
	if err != nil || actual.InputVersion == nil || len(actual.InputVersion.Slots) != 1000 {
		t.Fatal("legitimate delivered metadata was truncated", err)
	}
}
