package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestSavedRequestCommandsIgnoreCurrentConfiguration(t *testing.T) {
	data, root := privateTempDir(t), privateTempDir(t)
	app := testApp(t)
	app.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(data, "profiles"), nil }
	platformTestPaths(app, data)
	initializeTestVault(t, app)
	outputData(t, app, []string{"vault", "lock"})
	before := outputData(t, app, []string{"vault", "status"})
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("[broken\n"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Chdir(root)
	for _, group := range []string{"process", "project"} {
		id := tasks.ID()
		dir := "processes"
		receipt := map[string]any{"fingerprint": strings.Repeat("a", 64), "done": false}
		if group == "project" {
			dir = "project-operations"
			receipt = map[string]any{"fingerprint": strings.Repeat("b", 64), "action": "up", "profile": "app", "directory": root, "items": []any{}}
		}
		path := filepath.Join(data, dir, "receipts", id+".json")
		if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
			t.Fatal(e)
		}
		if e := tasks.WritePrivate(path, receipt); e != nil {
			t.Fatal(e)
		}
		got := outputData(t, app, []string{group, "request", "show", id})
		item := got["item"].(map[string]any)
		if item["request_id"] != id {
			t.Fatal(got)
		}
		code, _, diagnostic := invoke(t, app, "", group, "request", "show", tasks.ID())
		if code != 3 || !strings.Contains(diagnostic, "request_not_found") {
			t.Fatal(code, diagnostic)
		}
	}
	if after := outputData(t, app, []string{"vault", "status"}); !reflect.DeepEqual(before, after) {
		t.Fatal("request lookup touched vault session")
	}
}
