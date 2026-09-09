package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestTaskEditCLI(t *testing.T) {
	a := New("test", "test")
	dir := t.TempDir()
	a.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(dir, "profiles"), nil }
	run := func(input string, args ...string) map[string]any {
		t.Helper()
		var out, err bytes.Buffer
		if code := a.Run(context.Background(), args, IO{In: strings.NewReader(input), Out: &out, Err: &err}); code != 0 {
			t.Fatalf("%v: %d %s", args, code, err.String())
		}
		var env map[string]any
		if e := json.Unmarshal(out.Bytes(), &env); e != nil {
			t.Fatal(e)
		}
		return env["data"].(map[string]any)
	}
	w := run("", "task", "workstream", "create", "--profile", "test", "--title", "Editable", "--request-id", tasks.ID())
	id := w["item"].(map[string]any)["id"].(string)
	body := `{"reason":"Add work","operations":[{"op":"task.add","ref":"new","value":{"title":"Inserted"}}]}`
	args := []string{"task", "workstream", "edit", id, "--profile", "test", "--stdin", "--if-revision", fmt.Sprint(w["revision"])}
	preview := run(body, append(append([]string{}, args...), "--dry-run")...)
	if preview["changed"] != false || preview["would_change"] != true || preview["revision"] != w["revision"] {
		t.Fatal(preview)
	}
	commitArgs := append(append([]string{}, args...), "--request-id", tasks.ID())
	committed := run(body, commitArgs...)
	if committed["changed"] != true {
		t.Fatal(committed)
	}
	retried := run(body, commitArgs...)
	if retried["replayed"] != true || retried["revision"] != committed["revision"] {
		t.Fatal(retried)
	}
	schema := run("", "schema", "task", "workstream", "edit")
	raw, _ := json.Marshal(schema)
	for _, fragment := range []string{"task.move", "dry-run", "oneOf", "request-id"} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatal("missing schema contract", fragment)
		}
	}
}
