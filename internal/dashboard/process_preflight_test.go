package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestDashboardProcessStartPreflightRejectsBeforeExecutionRecord(t *testing.T) {
	data := t.TempDir()
	s := &Server{
		registry: Registry{Address: "http://127.0.0.1:1234"},
		data:     filepath.Join(data, "tasks"),
		sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)},
	}
	root := t.TempDir()
	config := "profile='app'\n[requirements]\nvars=['REQUIRED']\n[commands.web]\nexec=['/bin/true']\ninject=true\n"
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
