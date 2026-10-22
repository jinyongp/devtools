package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestDashboardProcessStartPreflightRejectsBeforeExecutionRecord(t *testing.T) {
	data := privateTempDir(t)
	s := &Server{
		registry: Registry{Address: "http://127.0.0.1:1234"},
		data:     filepath.Join(data, "tasks"),
		sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)},
	}
	root := privateTempDir(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal([]string{exe, "-test.run=^$"})
	config := "profile='app'\n[ports.web]\n[proxies.web]\nhost='${DASHBOARD_MISSING_NETWORK_DOMAIN}'\nport='web'\n[requirements]\nvars=['REQUIRED']\n[commands.web]\nserve=['web']\nexec=" + string(args) + "\ninject=true\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	requestID := tasks.ID()
	body, err := json.Marshal(actionRequest{
		Domain:  "process",
		Profile: "app",
		Process: &services.Request{Action: "start", Directory: root, Command: "web", RequestID: requestID},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", s.registry.Address+"/api/actions", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", s.registry.Address)
	request.Header.Set("Authorization", "Bearer session")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "\"code\":\"requirements_failed\"") {
		t.Fatalf("start: code=%d body=%s", response.Code, response.Body)
	}
	if _, err := os.Stat(filepath.Join(data, "processes", requestID, "record.json")); !os.IsNotExist(err) {
		t.Fatalf("execution record exists after failed preflight: %v", err)
	}
	state, portErr := (ports.Store{Directory: filepath.Join(data, "ports")}).Read()
	if portErr != nil || len(state.Instances) != 0 || len(state.Assignments) != 0 {
		t.Fatalf("failed preflight mutated port identity: %#v %v", state, portErr)
	}
}

func TestDashboardPreparesFrozenPublicInputBeforeManagedRestart(t *testing.T) {
	ctx := context.Background()
	data, root := privateTempDir(t), privateTempDir(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal([]string{exe, "-test.run=^$"})
	config := "profile='app'\n[commands.web]\ninject=true\nexec=" + string(args) + "\n"
	if err = os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	store := values.ModernStore{Directory: filepath.Join(data, "profiles"), Profile: "app"}
	if _, e := store.Update(ctx, func(s *values.Editor) (bool, *protocol.Error) {
		return s.Set(values.Variable, "URL", "", "https://before.invalid")
	}); e != nil {
		t.Fatal(e)
	}
	server := Server{data: filepath.Join(data, "tasks")}
	prepared, e := server.prepareManagedInput(ctx, services.Record{Profile: "app", Directory: root, Command: "web"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Release()
	if prepared.Input.Metadata().Values.Public == nil {
		t.Fatal("dashboard bypassed generation admission")
	}
	if _, e = store.Update(ctx, func(s *values.Editor) (bool, *protocol.Error) {
		return s.Set(values.Variable, "URL", "", "https://after.invalid")
	}); e != nil {
		t.Fatal(e)
	}
	actual, e := prepared.Input.Open()
	if e != nil {
		t.Fatal(e)
	}
	defer actual.Destroy()
	found := false
	for _, item := range actual.Environment {
		if item == "URL=https://before.invalid" {
			found = true
		}
	}
	if !found {
		t.Fatal("dashboard did not freeze actual input")
	}
}
