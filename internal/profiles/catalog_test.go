package profiles

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func writeStoredProfile(t *testing.T, data, domain, profile string) {
	t.Helper()
	directory := filepath.Join(data, domain)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, hex.EncodeToString([]byte(profile))+".json")
	body := []byte(`{"version":1,"profile":"` + profile + `","envs":{},"keys":{}}`)
	if domain == "tasks" {
		body = []byte(`{"version":1,"profile":"` + profile + `","events":[],"receipts":{},"contexts":{}}`)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeProcessRecord(t *testing.T, data, profile string) {
	t.Helper()
	id := tasks.ID()
	directory := filepath.Join(data, "processes", id)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	record := services.Record{ID: id, Profile: profile, Directory: filepath.Join(data, "project"), Command: "web", Env: "local", State: "running"}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "record.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogListsAllProfileSourcesOnceInOrder(t *testing.T) {
	data := t.TempDir()
	writeStoredProfile(t, data, "profiles", "zeta")
	writeStoredProfile(t, data, "profiles", "shared")
	writeStoredProfile(t, data, "tasks", "alpha")
	writeStoredProfile(t, data, "tasks", "shared")

	portStore := ports.Store{Directory: filepath.Join(data, "ports")}
	if err := portStore.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		if _, registerErr := state.Register("port-only", filepath.Join(data, "port-project")); registerErr != nil {
			return false, registerErr
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	writeProcessRecord(t, data, "process-only")
	writeProcessRecord(t, data, "shared")

	items, err := (Catalog{Data: data}).Names()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "port-only", "process-only", "shared", "zeta"}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("profiles mismatch: got %#v want %#v", items, want)
	}
}

func TestCatalogListDoesNotCreateMaintenanceState(t *testing.T) {
	data := t.TempDir()
	directory := filepath.Join(data, "profiles")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, hex.EncodeToString([]byte("passive"))+".json")
	if err := os.WriteFile(path, []byte(`{"version":1,"profile":"passive","envs":{"local":true},"keys":{}}`), 0600); err != nil {
		t.Fatal(err)
	}

	items, err := (Catalog{Data: data}).List()
	if err != nil || len(items) != 1 || items[0].Profile != "passive" || items[0].EnvCount != 1 {
		t.Fatalf("unexpected passive catalog result: %#v %v", items, err)
	}
	if _, err := os.Lstat(filepath.Join(data, ".maintenance")); !os.IsNotExist(err) {
		t.Fatalf("profile list created maintenance state: %v", err)
	}
}

func TestInspectRuntimeMetadataForPartialProfile(t *testing.T) {
	data := t.TempDir()
	portStore := ports.Store{Directory: filepath.Join(data, "ports")}
	if err := portStore.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		if _, registerErr := state.Register("runtime", filepath.Join(data, "runtime-project")); registerErr != nil {
			return false, registerErr
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	writeProcessRecord(t, data, "runtime")

	detail, err := (Catalog{Data: data}).Inspect(context.Background(), "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Profile != "runtime" || detail.Values || detail.Tasks || detail.InstanceCount != 1 || detail.ProcessCount != 1 || detail.ActiveProcessCount != 1 {
		t.Fatalf("unexpected runtime summary: %#v", detail.Summary)
	}
	if len(detail.Envs) != 0 || len(detail.Variables) != 0 || len(detail.Secrets) != 0 || len(detail.TaskCounts) != 0 {
		t.Fatalf("unexpected stored metadata: %#v", detail)
	}
	if len(detail.Instances) != 1 || detail.Instances[0].Directory != filepath.Join(data, "runtime-project") {
		t.Fatalf("unexpected instances: %#v", detail.Instances)
	}
	if len(detail.Processes) != 1 || detail.Processes[0].Command != "web" || detail.Processes[0].Env != "local" || detail.Processes[0].State != "interrupted" {
		t.Fatalf("unexpected processes: %#v", detail.Processes)
	}
}

func TestCatalogRejectsMalformedProfileStorage(t *testing.T) {
	data := t.TempDir()
	directory := filepath.Join(data, "profiles")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "not-hex.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Catalog{Data: data}).Names(); err == nil || err.Code != "invalid_storage" {
		t.Fatalf("expected invalid_storage, got %#v", err)
	}
}

func TestCatalogListRejectsCorruptTaskStorage(t *testing.T) {
	data := t.TempDir()
	directory := filepath.Join(data, "tasks")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, hex.EncodeToString([]byte("broken"))+".json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Catalog{Data: data}).List(); err == nil {
		t.Fatal("corrupt task storage was accepted by profile list")
	}
}

func TestCatalogRejectsNonPrivateProfileStorage(t *testing.T) {
	data := t.TempDir()
	directory := filepath.Join(data, "tasks")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := (Catalog{Data: data}).Names(); err == nil || err.Code != "storage_error" {
		t.Fatalf("expected storage_error, got %#v", err)
	}
}
