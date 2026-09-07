package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestDynamicCompletion(t *testing.T) {
	a := testApp(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	if err := os.WriteFile("devtools.toml", []byte("profile = 'app'\n[commands.web]\nexec = ['echo', 'CANARY-VALUE']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	call := func(input string, args ...string) map[string]any {
		t.Helper()
		code, out, stderr := invoke(t, a, input, args...)
		if code != 0 {
			t.Fatalf("setup: %v %d %s", args, code, stderr)
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	call("", "env", "create", "local")
	call("", "env", "create", "other", "--profile", "other")
	call("", "var", "set", "_LEVEL", "--value", "CANARY-VALUE")
	call("", "var", "set", "LOCAL_ONLY", "--env", "local", "--value", "CANARY-VALUE")
	call("CANARY-SECRET", "sec", "set", "TOKEN", "--stdin")
	workstream := call("", "task", "workstream", "create", "--title", "CANARY-TITLE", "--request-id", tasks.ID())["item"].(map[string]any)["id"].(string)
	taskID := call("", "task", "add", "--title", "CANARY-TITLE", "--workstream", workstream, "--request-id", tasks.ID())["item"].(map[string]any)["id"].(string)
	otherTask := call("", "task", "add", "--title", "CANARY-TITLE", "--request-id", tasks.ID())["item"].(map[string]any)["id"].(string)
	runID := call("", "task", "claim", otherTask, "--request-id", tasks.ID())["run"].(map[string]any)["id"].(string)
	data, _ := a.dataDirectory()
	snapshot := func() map[string]string {
		out := map[string]string{}
		err := filepath.WalkDir(filepath.Dir(data), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				out[path] = "dir"
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[path] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := snapshot()
	cases := []struct {
		words []string
		want  []string
	}{
		{[]string{"var", "list", "--profile", ""}, []string{"app", "other"}},
		{[]string{"var", "list", "--profile", "ot"}, []string{"other"}},
		{[]string{"var", "list", "--profile=ot"}, []string{"--profile=other"}},
		{[]string{"var", "list", "--env", ""}, []string{"local"}},
		{[]string{"var", "list", "--profile", "other", "--env", ""}, []string{"other"}},
		{[]string{"var", "list", "--profile", "=", "other", "--env", ""}, []string{"other"}},
		{[]string{"var", "list", "--profile=other", "--env=o"}, []string{"--env=other"}},
		{[]string{"env", "remove", ""}, []string{"local"}},
		{[]string{"variable", "get", ""}, []string{"_LEVEL"}},
		{[]string{"variable", "get", "--env", "local", ""}, []string{"LOCAL_ONLY", "_LEVEL"}},
		{[]string{"sec", "unset", ""}, []string{"TOKEN"}},
		{[]string{"run", ""}, []string{"web"}},
		{[]string{"process", "start", "--dir", projectDir, ""}, []string{"web"}},
		{[]string{"task", "show", taskID[:8]}, []string{taskID}},
		{[]string{"task", "show", "--workstream", workstream, ""}, []string{taskID}},
		{[]string{"task", "add", "--workstream", ""}, []string{workstream}},
		{[]string{"task", "workstream", "show", ""}, []string{workstream}},
		{[]string{"task", "workstream", "depends", "set", workstream, "--depends-on", ""}, nil},
		{[]string{"task", "checkpoint", ""}, []string{runID}},
		{[]string{"task", "checkpoint", "list", ""}, []string{runID}},
		{[]string{"var", "set", "KEY", "--value", ""}, nil},
		{[]string{"sec", "set", "KEY", "--file", ""}, nil},
		{[]string{"run", "--", ""}, nil},
		{[]string{"var", "get", "--profile", "missing", ""}, nil},
		{[]string{"var", "get", "--env", "missing", ""}, nil},
		{[]string{"task", "add", "--request-id", ""}, nil},
		{[]string{"task", "show", otherTask, ""}, nil},
	}
	for _, tc := range cases {
		code, out, stderr := invoke(t, a, strings.Join(tc.words, "\x00")+"\x00", "__complete")
		want := strings.Join(tc.want, "\n")
		if want != "" {
			want += "\n"
		}
		if code != 0 || stderr != "" || out != want {
			t.Fatalf("%v: code=%d out=%q err=%q want=%q", tc.words, code, out, stderr, want)
		}
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("completion changed persistent data")
	}
	for _, input := range []string{"", "malformed", strings.Repeat("x", 65537)} {
		code, out, stderr := invoke(t, a, input, "__complete")
		if code != 0 || out != "" || stderr != "" {
			t.Fatalf("malformed input: %d %q %q", code, out, stderr)
		}
	}
}

func TestCompletionDeadline(t *testing.T) {
	a := testApp(t)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	start := time.Now()
	if code := a.completeDynamic(context.Background(), IO{In: reader, Out: io.Discard, Err: io.Discard}); code != 0 {
		t.Fatal(code)
	}
	if time.Since(start) > time.Second {
		t.Fatal("completion waited on unbounded input")
	}
	dir, _ := a.dataDirectory()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("completion created storage")
	}
}
