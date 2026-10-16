package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestTaskGraphCLIUsesEmittedZeroDepthContinuation(t *testing.T) {
	app := New("test", "test")
	root := privateTempDir(t)
	run := func(body string, args ...string) map[string]any {
		t.Helper()
		var out, diagnostic bytes.Buffer
		args = append([]string{"--isolated-root", root}, args...)
		args = append(args, "--profile", "test")
		if code := app.Run(context.Background(), args, IO{In: strings.NewReader(body), Out: &out, Err: &diagnostic}); code != 0 {
			t.Fatalf("%v: %d %s", args, code, diagnostic.String())
		}
		var response struct {
			Data map[string]any `json:"data"`
		}
		if e := json.Unmarshal(out.Bytes(), &response); e != nil {
			t.Fatal(e)
		}
		return response.Data
	}
	ids := []string{}
	var last map[string]any
	for _, title := range []string{"deep", "prerequisite", "dependent"} {
		last = run("", "task", "workstream", "create", "--title", title, "--request-id", tasks.ID())
		ids = append(ids, last["item"].(map[string]any)["id"].(string))
	}
	for i := 1; i < len(ids); i++ {
		last = run(fmt.Sprintf(`{"depends_on":["%s"]}`, ids[i-1]), "task", "workstream", "depends", "set", ids[i], "--stdin", "--if-revision", fmt.Sprint(last["revision"]), "--request-id", tasks.ID())
	}
	first := run("", "task", "workstream", "tree", ids[2], "--depth", "1", "--limit", "1")
	continuations := first["continuations"].([]any)
	if len(continuations) != 1 {
		t.Fatal(first)
	}
	continuation := continuations[0].(map[string]any)
	next := run("", "task", "workstream", "tree", continuation["id"].(string), "--depth", fmt.Sprint(continuation["depth"]), "--direction", continuation["direction"].(string), "--cursor", first["cursor"].(string), "--limit", "1")
	nodes := next["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["id"] != ids[1] || next["truncated"] != false || next["cursor"] != nil || len(next["edges"].([]any)) != 0 || len(next["continuations"].([]any)) != 0 || next["revision"] != first["revision"] {
		t.Fatal("emitted continuation did not finish at its boundary", next)
	}
}
