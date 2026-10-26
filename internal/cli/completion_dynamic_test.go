package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	proxyapi "github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestDynamicCompletion(t *testing.T) {
	a := testApp(t)
	initializeTestVault(t, a)
	managerData, _ := a.dataDirectory()
	// Completion reads metadata; configuring its fixture must not probe unrelated
	// host services occupying the example listener ports.
	a.proxyManager = func(string) proxyapi.Manager { return cliTestManager(filepath.Dir(managerData)) }
	a.portProbe = managerPortStore(cliTestManager(filepath.Dir(managerData))).Probe
	projectDir := privateTempDir(t)
	t.Chdir(projectDir)
	if err := os.WriteFile("devtools.toml", []byte("profile = 'app'\n[tools.echo]\nexecutable = 'echo'\n[presets.base]\ntools = ['echo']\n[ports.web]\nport = 38082\n[proxies.web]\nhost = 'web.localhost'\nport = 'web'\n[commands.web]\nexec = ['echo', 'CANARY-VALUE']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := range a.commands {
		if a.commands[i].Name == "process start" {
			a.commands[i].Arguments[0].Repeatable = true
		}
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
	bundleRevision := call("", "bundle", "list")["revision"].(string)
	call("", "bundle", "set", "dev-bundle", "--key", "_LEVEL", "--revision", bundleRevision, "--request-id", tasks.ID())
	workstream := call("", "task", "workstream", "create", "--title", "CANARY-TITLE", "--request-id", tasks.ID())["item"].(map[string]any)["id"].(string)
	taskID := call("", "task", "add", "--title", "CANARY-TITLE", "--workstream", workstream, "--request-id", tasks.ID())["item"].(map[string]any)["id"].(string)
	otherTask := call("", "task", "add", "--title", "CANARY-TITLE", "--request-id", tasks.ID())["item"].(map[string]any)["id"].(string)
	runID := call("", "task", "claim", otherTask, "--request-id", tasks.ID())["run"].(map[string]any)["id"].(string)
	call("", "proxy", "configure", "--http-address", "127.0.0.1:38080", "--request-id", tasks.ID())
	call("", "proxy", "configure", "--listener", "preview", "--http-address", "127.0.0.1:38081", "--request-id", tasks.ID())
	call("", "proxy", "add", "shared", "38083", "--global", "--host", "shared.localhost", "--request-id", tasks.ID())
	instanceID := call("", "port", "allocate", "web")["item"].(map[string]any)["instance_id"].(string)
	call("", "instance", "name", "current")
	// A record with no supervisor endpoint must still complete without RPC or
	// interpreting the caller's project as its owner.
	processID := tasks.ID()
	processDir := filepath.Join(filepath.Dir(managerData), "processes", processID)
	if err := os.MkdirAll(processDir, 0700); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(services.Record{ID: processID, Profile: "other", Command: "worker", Directory: projectDir, State: "running", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDir, "record.json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	// A new command/option needs only entity metadata; the resolver must not
	// contain its name or infer a source from its argument spelling.
	a.commands = append(a.commands, Command{Name: "fixture choose", Arguments: []Argument{
		{Name: "source", Completion: completeCommand}, {Name: "destination"},
	}, Options: []Option{{Name: "owner", Completion: completeProfile}}})
	call("", "vault", "lock")
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
			if entry.Type()&os.ModeSocket != 0 {
				out[path] = "socket"
				return nil
			}
			// Windows byte-range locks prohibit reading live daemon lock bytes.
			// Observe their metadata while retaining their names in the snapshot.
			if strings.HasSuffix(entry.Name(), ".lock") {
				info, err := entry.Info()
				if err != nil {
					return err
				}
				out[path] = info.ModTime().String()
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
		{[]string{"command", "inspect", "--dir", projectDir, ""}, []string{"web"}},
		{[]string{"command", "explain", ""}, []string{"web"}},
		{[]string{"command", "explain", "--profile", "other", ""}, []string{"web"}},
		{[]string{"fixture", "choose", ""}, []string{"web"}},
		{[]string{"fixture", "choose", "web", ""}, nil},
		{[]string{"fixture", "choose", "--owner", ""}, []string{"app", "other"}},
		{[]string{"command", "explain", "--dir", projectDir, "w"}, []string{"web"}},
		{[]string{"command", "update", ""}, []string{"web"}},
		{[]string{"command", "remove", ""}, []string{"web"}},
		{[]string{"command", "rename", ""}, []string{"web"}},
		{[]string{"command", "rename", "web", ""}, nil},
		{[]string{"command", "add", ""}, nil},
		{[]string{"config", "tool", "update", ""}, []string{"echo"}},
		{[]string{"config", "tool", "remove", ""}, []string{"echo"}},
		{[]string{"config", "tool", "add", ""}, nil},
		{[]string{"config", "preset", "update", ""}, []string{"base"}},
		{[]string{"config", "preset", "remove", ""}, []string{"base"}},
		{[]string{"config", "preset", "add", ""}, nil},
		{[]string{"profile", "inspect", ""}, []string{"app", "other"}},
		{[]string{"profile", "diff", ""}, []string{"app", "other"}},
		{[]string{"profile", "diff", "app", ""}, []string{"app", "other"}},
		{[]string{"bundle", "remove", ""}, []string{"dev-bundle"}},
		{[]string{"bundle", "rename", "dev-bundle", ""}, nil},
		{[]string{"bundle", "set", "new", "--key", ""}, []string{"TOKEN", "_LEVEL"}},
		{[]string{"values", "inspect", "--bundle", ""}, []string{"all", "dev-bundle", "none"}},
		{[]string{"command", "explain", "web", "--bundle=d"}, []string{"--bundle=dev-bundle"}},
		{[]string{"run", "--bundle", ""}, []string{"all", "dev-bundle", "none"}},
		{[]string{"run", "--profile", "other", "--bundle", ""}, []string{"all", "none"}},
		{[]string{"import", "--bundle", ""}, []string{"dev-bundle"}},
		{[]string{"run", "--output", ""}, []string{"auto", "raw", "redacted"}},
		{[]string{"run", "--output=r"}, []string{"--output=raw", "--output=redacted"}},
		{[]string{"doctor", "--mode", ""}, []string{"foreground", "managed"}},
		{[]string{"task", "list", "--state", ""}, []string{"all", "canceled", "done", "open"}},
		{[]string{"port", "allocate", ""}, []string{"web"}},
		{[]string{"port", "release", "--profile", "app", "--instance", "current", ""}, []string{"web"}},
		{[]string{"instance", "remove", ""}, []string{"current", "id:" + instanceID}},
		{[]string{"instance", "move", "--dir", privateTempDir(t), ""}, []string{"current", "id:" + instanceID}},
		{[]string{"port", "show", "--profile", "other", "--instance", ""}, nil},
		{[]string{"process", "status", ""}, []string{processID}},
		{[]string{"process", "restart", processID, "--env", ""}, nil},
		{[]string{"process", "restart", processID, "--bundle", ""}, nil},
		{[]string{"command", "env", ""}, []string{"web"}},
		{[]string{"proxy", "env", ""}, []string{"web"}},
		{[]string{"proxy", "env", "--global", ""}, []string{"shared"}},
		{[]string{"proxy", "run", "--global", "sh"}, []string{"shared"}},
		{[]string{"proxy", "start", "--listener", ""}, []string{"default", "preview"}},
		{[]string{"proxy", "stop", "--listener=pre"}, []string{"--listener=preview"}},
		{[]string{"expose", "start", "web", "--provider", ""}, []string{"cloudflared", "lan", "local", "tailscale"}},
		{[]string{"proxy", "add", "shared", "38083", "--global", "--dns", ""}, []string{"auto", "external", "hosts"}},
		{[]string{"command", "run", ""}, []string{"web"}},
		{[]string{"run", ""}, []string{"web"}},
		{[]string{"run", "--env", ""}, []string{"local"}},
		{[]string{"run", "web", "--env", ""}, nil},
		{[]string{"command", "run", "web", "--profile", ""}, nil},
		{[]string{"cmd", "run", "web", "--env=o"}, nil},
		{[]string{"process", "start", "--dir", projectDir, ""}, []string{"web"}},
		{[]string{"process", "start", "--dir", projectDir, "web", ""}, []string{"web"}},
		{[]string{"project", "up", ""}, []string{"web"}},
		{[]string{"project", "up", "web", ""}, nil},
		{[]string{"project", "status", "web", ""}, nil},
		{[]string{"project", "down", "web", ""}, nil},
		{[]string{"task", "show", taskID[:8]}, []string{taskID}},
		{[]string{"task", "show", "--workstream", workstream, ""}, []string{taskID}},
		{[]string{"task", "add", "--workstream", ""}, []string{workstream}},
		{[]string{"task", "workstream", "show", ""}, []string{workstream}},
		{[]string{"task", "workstream", "depends", "set", workstream, "--depends-on", ""}, nil},
		{[]string{"task", "checkpoint", ""}, []string{runID}},
		{[]string{"task", "checkpoint", "list", ""}, []string{runID}},
		{[]string{"var", "set", "KEY", "--value", ""}, nil},
		{[]string{"var", "set", "KEY", "--file", ""}, nil},
		{[]string{"sec", "set", "KEY", "--file", ""}, nil},
		{[]string{"run", "--", ""}, nil},
		{[]string{"var", "get", "--profile", "missing", ""}, nil},
		{[]string{"var", "get", "--env", "missing", ""}, nil},
		{[]string{"task", "add", "--request-id", ""}, nil},
		{[]string{"task", "show", otherTask, ""}, nil},
	}
	for _, tc := range cases {
		// Candidate correctness is independent of the shell protocol's best-effort
		// 200ms budget, which may expire during storage reads on a busy runner.
		got := a.dynamicCandidates(tc.words)
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Fatalf("%v: got=%q want=%q", tc.words, got, tc.want)
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

func TestCompletionProtocol(t *testing.T) {
	a := testApp(t)
	// A fixed provider list exercises NUL parsing, dispatch, sorting, filtering
	// and newline encoding without tying storage correctness to a latency bound.
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"expose\x00start\x00web\x00--provider\x00\x00", "cloudflared\nlan\nlocal\ntailscale\n"},
		{"expose\x00start\x00web\x00--provider\x00lo\x00", "local\n"},
		{"expose\x00start\x00web\x00--provider=lo\x00", "--provider=local\n"},
	} {
		code, out, stderr := invoke(t, a, tc.input, "__complete")
		if code != 0 || stderr != "" || out != tc.want {
			t.Fatalf("%q: code=%d out=%q err=%q want=%q", tc.input, code, out, stderr, tc.want)
		}
	}
}

func TestCompletionDeadline(t *testing.T) {
	a := testApp(t)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	var out, diagnostic bytes.Buffer
	start := time.Now()
	if code := a.completeDynamic(context.Background(), IO{In: reader, Out: &out, Err: &diagnostic}); code != 0 {
		t.Fatal(code)
	}
	if time.Since(start) > time.Second {
		t.Fatal("completion waited on unbounded input")
	}
	if out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("expired completion emitted output or diagnostics")
	}
	dir, _ := a.dataDirectory()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("completion created storage")
	}
}
