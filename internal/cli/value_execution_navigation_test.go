package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestValueExecutionCursorPreservesScopeAndRequiresExistingSession(t *testing.T) {
	a := testApp(t)
	t.Chdir(privateTempDir(t))
	initializeTestVault(t, a)
	if e := os.WriteFile("devtools.toml", []byte("profile='app-a'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	outputData(t, a, []string{"var", "set", "TOKEN", "--value", "synthetic-value"})
	for i := 0; i < 3; i++ {
		body := fmt.Sprintf(`{"external_execution_id":"job-%d","selection":{"keys":["TOKEN"]}}`, i)
		code, _, diagnostic := invoke(t, a, body, "values", "execution", "prepare", "--stdin", "--request-id", tasks.ID())
		if code != 0 {
			t.Fatal(code, diagnostic)
		}
	}
	cache := filepath.Join(privateTempDir(t), "snapshots")
	page := outputData(t, a, []string{"values", "execution", "list", "--profile", "app-a", "--view", "history", "--delivery-state", "prepared", "--limit", "1", "--query-cache-dir", cache})
	action := previewAction(t, page)
	if action.State != "ready" || a.ResolveAction(action.Argv) != nil || !strings.Contains(strings.Join(action.Argv, " "), "--delivery-state=prepared") {
		t.Fatal(action)
	}
	continued := outputData(t, a, action.Argv[1:])
	if len(continued["items"].([]any)) != 1 {
		t.Fatal(continued)
	}
	outputData(t, a, []string{"vault", "lock"})
	code, _, diagnostic := invoke(t, a, "", action.Argv[1:]...)
	var failure struct{ Error struct{ Code string } }
	if e := json.Unmarshal([]byte(diagnostic), &failure); e != nil {
		t.Fatal(e)
	}
	if code != 3 || failure.Error.Code != "vault_locked" {
		t.Fatal("snapshot bypassed protected metadata authorization", code, diagnostic)
	}
}
