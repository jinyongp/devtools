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
	updateArgs := []string{"task", "workstream", "update", id, "--profile", "test", "--title", "Metadata", "--description", "Updated goal", "--if-revision", fmt.Sprint(w["revision"]), "--request-id", tasks.ID()}
	updated := run("", updateArgs...)
	item := updated["item"].(map[string]any)
	if item["title"] != "Metadata" || item["description"] != "Updated goal" {
		t.Fatal("standalone metadata update missing", item)
	}
	if replayed := run("", updateArgs...); replayed["replayed"] != true || replayed["revision"] != updated["revision"] {
		t.Fatal("standalone metadata retry did not replay", replayed)
	}
	shown := run("", "task", "workstream", "show", id, "--profile", "test")
	if shown["item"].(map[string]any)["title"] != "Metadata" {
		t.Fatal("updated metadata missing from show", shown)
	}
	history := run("", "task", "workstream", "history", id, "--profile", "test")
	historyRaw, _ := json.Marshal(history)
	if !strings.Contains(string(historyRaw), "workstream.edited") {
		t.Fatal("canonical metadata event missing from history", string(historyRaw))
	}
	body := `{"reason":"Add work","operations":[{"op":"task.add","ref":"new","value":{"title":"Inserted"}}]}`
	args := []string{"task", "workstream", "edit", id, "--profile", "test", "--stdin", "--if-revision", fmt.Sprint(updated["revision"])}
	preview := run(body, append(append([]string{}, args...), "--dry-run")...)
	if preview["changed"] != false || preview["would_change"] != true || preview["revision"] != updated["revision"] {
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
	for _, fragment := range []string{"workstream.update", "task.move", "dry-run", "oneOf", "request-id"} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatal("missing schema contract", fragment)
		}
	}
	standalone := run("", "schema", "task", "workstream", "update")
	standaloneRaw, _ := json.Marshal(standalone)
	if !strings.Contains(string(standaloneRaw), `"minProperties":1`) {
		t.Fatal("standalone metadata schema allows an empty update", string(standaloneRaw))
	}
}
