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
	var noChangeOut, noChangeErr bytes.Buffer
	noChangeArgs := []string{"task", "workstream", "update", id, "--profile", "test", "--title", "Metadata", "--if-revision", fmt.Sprint(updated["revision"]), "--request-id", tasks.ID()}
	if code := a.Run(context.Background(), noChangeArgs, IO{In: strings.NewReader(""), Out: &noChangeOut, Err: &noChangeErr}); code != 3 || noChangeOut.Len() != 0 {
		t.Fatalf("no_change exit=%d stdout=%s stderr=%s", code, &noChangeOut, &noChangeErr)
	}
	var noChangeEnvelope map[string]any
	if e := json.Unmarshal(noChangeErr.Bytes(), &noChangeEnvelope); e != nil {
		t.Fatal(e)
	}
	noChangeError := noChangeEnvelope["error"].(map[string]any)
	noChangeDetails := noChangeError["details"].(map[string]any)
	if noChangeError["code"] != "no_change" || noChangeDetails["affected_count"] != float64(0) {
		t.Fatal("no_change diagnostics missing", noChangeEnvelope)
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
	for _, fragment := range []string{`"minProperties":1`, `"previous_revision"`, `"affected_count"`, `"affected_ids"`} {
		if !strings.Contains(string(standaloneRaw), fragment) {
			t.Fatal("standalone mutation schema is incomplete", fragment, string(standaloneRaw))
		}
	}
}

func TestTaskUpdateUsesEnvironmentContextAsGuard(t *testing.T) {
	a := New("test", "test")
	dir := t.TempDir()
	a.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(dir, "profiles"), nil }
	var out, diagnostic bytes.Buffer
	requestID := tasks.ID()
	if code := a.Run(context.Background(), []string{"task", "add", "--profile", "test", "--title", "Guarded", "--request-id", requestID}, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic}); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	var created map[string]any
	if e := json.Unmarshal(out.Bytes(), &created); e != nil {
		t.Fatal(e)
	}
	data := created["data"].(map[string]any)
	id := data["item"].(map[string]any)["id"].(string)
	t.Setenv("DEVTOOLS_TASK_CONTEXT", "unknown-context")
	out.Reset()
	diagnostic.Reset()
	args := []string{"task", "update", id, "--profile", "test", "--title", "Changed", "--if-revision", fmt.Sprint(data["revision"]), "--request-id", tasks.ID()}
	if code := a.Run(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic}); code != 3 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &out, &diagnostic)
	}
	if !strings.Contains(diagnostic.String(), `"code":"context_invalid"`) || !strings.Contains(diagnostic.String(), `"context_reason":"unknown"`) || strings.Contains(diagnostic.String(), "unknown-context") {
		t.Fatal("environment context guard diagnostics", diagnostic.String())
	}
}

func TestContextFreeTaskCommandIgnoresEnvironmentContext(t *testing.T) {
	a := New("test", "test")
	dir := t.TempDir()
	a.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(dir, "profiles"), nil }
	run := func(args ...string) map[string]any {
		t.Helper()
		var out, diagnostic bytes.Buffer
		if code := a.Run(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic}); code != 0 {
			t.Fatalf("%v: %d %s", args, code, diagnostic.String())
		}
		var envelope map[string]any
		if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
			t.Fatal(e)
		}
		return envelope["data"].(map[string]any)
	}

	requestID := tasks.ID()
	args := []string{"task", "add", "--profile", "test", "--title", "Context free", "--request-id", requestID}
	t.Setenv("DEVTOOLS_TASK_CONTEXT", "ambient-old")
	created := run(args...)
	t.Setenv("DEVTOOLS_TASK_CONTEXT", "ambient-new")
	if replayed := run(args...); replayed["replayed"] != true || replayed["revision"] != created["revision"] {
		t.Fatal("ambient context changed a context-free receipt", replayed)
	}

	addSchema := run("schema", "task", "add")
	addInput := addSchema["input_schema"].(map[string]any)
	addProperties := addInput["properties"].(map[string]any)
	if _, exists := addProperties["context"]; exists {
		t.Fatal("context-free command advertises context")
	}
	validationSchema := run("schema", "task", "validation", "basis")
	validationInput := validationSchema["input_schema"].(map[string]any)
	validationProperties := validationInput["properties"].(map[string]any)
	if _, exists := validationProperties["context"]; !exists {
		t.Fatal("task-authorized validation does not advertise context")
	}
}
