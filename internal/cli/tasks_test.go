package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskCLIAndSchema(t *testing.T) {
	a := New("test", "test")
	dir := t.TempDir()
	a.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(dir, "profiles"), nil }
	run := func(input string, args ...string) map[string]any {
		t.Helper()
		var out, err bytes.Buffer
		code := a.Run(context.Background(), args, IO{In: strings.NewReader(input), Out: &out, Err: &err})
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, err.String())
		}
		var envelope map[string]any
		if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
			t.Fatal(e)
		}
		return envelope["data"].(map[string]any)
	}
	o := run("", "task", "add", "--profile", "test", "--title", "CLI task", "--request-id", tasks.ID())
	id := o["item"].(map[string]any)["id"].(string)
	claim := run("", "task", "claim", id, "--profile", "test", "--request-id", tasks.ID())
	runID := claim["run"].(map[string]any)["id"].(string)
	run(`{"summary":"Saved progress"}`, "task", "checkpoint", runID, "--stdin", "--profile", "test", "--context", claim["context"].(string), "--request-id", tasks.ID())
	rows := run("", "task", "checkpoint", "list", runID, "--profile", "test")
	if len(rows["items"].([]any)) != 1 {
		t.Fatal("longest command match")
	}
	catalog := run("", "schema")
	found := false
	for _, raw := range catalog["commands"].([]any) {
		c := raw.(map[string]any)
		if c["name"] == "task workstream spec set" {
			found = c["body_schema"] != nil
		}
	}
	if !found {
		t.Fatal("task body schema missing")
	}
}

func TestLongAlias(t *testing.T) {
	a := New("test", "test")
	var out, err bytes.Buffer
	if code := a.Run(context.Background(), []string{"dashboard", "start", "--help"}, IO{In: strings.NewReader(""), Out: &out, Err: &err}); code != 0 {
		t.Fatal(code, err.String())
	}
	if !strings.Contains(out.String(), `"name":"dashboard"`) {
		t.Fatal(out.String())
	}
}
