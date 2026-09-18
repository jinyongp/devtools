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
	var mismatchOut, mismatchErr bytes.Buffer
	contextToken := claim["context"].(string)
	code := a.Run(context.Background(), []string{"task", "checkpoint", id, "--summary", "Wrong target", "--profile", "test", "--context", contextToken, "--request-id", tasks.ID()}, IO{In: strings.NewReader(""), Out: &mismatchOut, Err: &mismatchErr})
	if code != 3 || mismatchOut.Len() != 0 || strings.Contains(mismatchErr.String(), contextToken) {
		t.Fatalf("unsafe checkpoint mismatch: exit=%d stdout=%s stderr=%s", code, mismatchOut.String(), mismatchErr.String())
	}
	var mismatchEnvelope struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(mismatchErr.Bytes(), &mismatchEnvelope); err != nil || mismatchEnvelope.Error.Code != "context_invalid" || mismatchEnvelope.Error.Details["expected_target_kind"] != "run" || mismatchEnvelope.Error.Details["actual_target_kind"] != "task" {
		t.Fatal("checkpoint mismatch contract", mismatchErr.String(), err)
	}
	run(`{"summary":"Saved progress"}`, "task", "checkpoint", runID, "--stdin", "--profile", "test", "--context", claim["context"].(string), "--request-id", tasks.ID())
	rows := run("", "task", "checkpoint", "list", runID, "--profile", "test")
	if len(rows["items"].([]any)) != 1 {
		t.Fatal("longest command match")
	}
	catalog := run("", "schema", "--all")
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

func TestTaskHelpNamesTargetKinds(t *testing.T) {
	a := New("test", "test")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"task", "checkpoint", "--help"}, "<run-id>"},
		{[]string{"task", "checkpoint", "list", "--help"}, "<run-id>"},
		{[]string{"task", "done", "--help"}, "<task-id>"},
		{[]string{"task", "workstream", "show", "--help"}, "<workstream-id>"},
		{[]string{"task", "validation", "record", "--help"}, "<validation-id>"},
	} {
		var out, diagnostic bytes.Buffer
		if code := a.Run(context.Background(), tc.args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic}); code != 0 {
			t.Fatalf("%v: %d %s", tc.args, code, diagnostic.String())
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("%v: want %q in %q", tc.args, tc.want, out.String())
		}
	}
}

func TestCLIIdentifierAndDescriptionContracts(t *testing.T) {
	a := New("test", "test")
	wantArgument := map[string]string{
		"process status": "execution-id", "process stop": "execution-id", "process restart": "execution-id",
		"process logs": "execution-id", "process check": "execution-id", "process wait": "execution-id",
		"cleanup apply": "plan-id", "cleanup restore": "archive-id", "cleanup purge": "archive-id",
		"port show": "port-name", "port allocate": "port-name", "port check": "port-name", "port release": "port-name",
		"instance name": "alias", "instance move": "instance-selector", "instance remove": "instance-selector",
	}
	run := func(args ...string) []byte {
		t.Helper()
		var out, diagnostic bytes.Buffer
		if code := a.Run(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic}); code != 0 {
			t.Fatalf("%v: %d %s", args, code, diagnostic.String())
		}
		return out.Bytes()
	}
	for command, want := range wantArgument {
		words := strings.Fields(command)
		help := run(append(words, "--help")...)
		if !bytes.Contains(help, []byte("<"+want+">")) {
			t.Errorf("%s help does not name <%s>: %s", command, want, help)
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(run(append([]string{"schema"}, words...)...), &envelope); err != nil {
			t.Fatal(command, err)
		}
		input := envelope.Data["input_schema"].(map[string]any)
		args := input["properties"].(map[string]any)["args"].(map[string]any)
		argument := args["prefixItems"].([]any)[0].(map[string]any)
		if argument["description"] != want {
			t.Errorf("%s schema argument = %v, want %s", command, argument["description"], want)
		}
		if strings.HasSuffix(want, "-id") && argument["pattern"] != uuidPattern {
			t.Errorf("%s schema does not validate its UUID argument", command)
		}
	}
	var catalogEnvelope struct {
		Data struct {
			Commands  []map[string]any  `json:"commands"`
			ExitCodes map[string]string `json:"exit_codes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(run("schema", "--all"), &catalogEnvelope); err != nil {
		t.Fatal(err)
	}
	wantQueryOptions := map[string][]string{
		"task show":                 {"profile", "workstream"},
		"task next":                 {"profile", "workstream"},
		"task current":              {"profile", "limit", "cursor", "dir"},
		"task list":                 {"profile", "state", "workstream", "limit", "cursor", "scope", "completion"},
		"task workstream list":      {"profile", "state", "limit", "cursor", "completion"},
		"task validation list":      {"profile", "workstream", "limit", "cursor", "scope"},
		"task tree":                 {"profile", "workstream", "limit", "cursor", "direction", "depth"},
		"task workstream tree":      {"profile", "limit", "cursor", "direction", "depth"},
		"task history":              {"profile", "workstream", "limit", "cursor"},
		"task checkpoint list":      {"profile", "limit", "cursor"},
		"task impact":               {"profile", "workstream", "file", "stdin"},
		"task workstream plan show": {"profile", "at-revision"},
	}
	wantQueryPatterns := map[string]map[string]string{
		"task list": {
			"state": `^(open|done|canceled|all)$`, "limit": `^([1-9]|[1-9][0-9]|1[0-9]{2}|200)$`,
			"scope": `^(included|removed|all)$`, "completion": `^(none|current|stale|all)$`,
		},
		"task workstream list": {
			"state": `^(draft|active|done|canceled|all)$`, "limit": `^([1-9]|[1-9][0-9]|1[0-9]{2}|200)$`,
		},
		"task tree": {
			"direction": `^(upstream|downstream)$`, "depth": `^([1-9]|1[0-9]|20)$`,
		},
		"task workstream plan show": {"at-revision": positiveIntegerPattern},
	}
	for _, command := range catalogEnvelope.Data.Commands {
		name, _ := command["name"].(string)
		if !strings.HasPrefix(name, "task ") {
			continue
		}
		description, _ := command["description"].(string)
		words := strings.Fields(description)
		internalAction := len(words) > 1 && strings.HasPrefix(description, "Apply ") && strings.Contains(words[1], ".")
		if description == "" || internalAction || strings.HasPrefix(description, "Read task domain:") {
			t.Errorf("internal task description exposed by %s: %q", name, description)
		}
		for _, raw := range command["options"].([]any) {
			option := raw.(map[string]any)
			description, _ := option["description"].(string)
			if description == "" || strings.HasPrefix(description, "Request field:") || strings.HasPrefix(description, "Query option:") {
				t.Errorf("internal option description exposed by %s --%s: %q", name, option["name"], description)
			}
			if option["name"] == "workstream" && name == "task validation list" && description != "Filter results by workstream UUID." {
				t.Errorf("validation workstream filter description = %q", description)
			}
			if option["name"] == "workstream" && name == "task show" && !strings.Contains(description, "completion") {
				t.Errorf("task show completion hint description = %q", description)
			}
			if option["name"] == "workstream" && name == "task tree" && !strings.Contains(description, "when no task UUID is supplied") {
				t.Errorf("task tree root filter description = %q", description)
			}
			if option["name"] == "state" {
				wantStates := "open, done, canceled, or all"
				if name == "task workstream list" {
					wantStates = "draft, active, done, canceled, or all"
				}
				if !strings.Contains(description, wantStates) {
					t.Errorf("%s state description = %q, want %q", name, description, wantStates)
				}
			}
			wantDefault := map[string]string{"limit": "defaults to 50", "direction": "defaults to upstream", "depth": "defaults to 3", "dir": "defaults to the current directory"}
			if option["name"] == "dir" && name != "task current" {
				delete(wantDefault, "dir")
			}
			if phrase := wantDefault[option["name"].(string)]; phrase != "" && !strings.Contains(description, phrase) {
				t.Errorf("%s --%s description = %q, want %q", name, option["name"], description, phrase)
			}
			if patterns := wantQueryPatterns[name]; patterns != nil {
				if pattern, exists := patterns[option["name"].(string)]; exists && option["pattern"] != pattern {
					t.Errorf("%s --%s pattern = %v, want %s", name, option["name"], option["pattern"], pattern)
				}
			}
			if name == "task validation record" && option["name"] == "result" && option["pattern"] != `^(pass|fail|blocked|skipped)$` {
				t.Errorf("validation result pattern = %v", option["pattern"])
			}
			if name == "task checkpoint" && option["name"] == "compaction-fingerprint" && option["pattern"] != `^[0-9a-f]{64}$` {
				t.Errorf("compaction fingerprint pattern = %v", option["pattern"])
			}
			if name == "task checkpoint" && option["name"] == "compaction-through" && option["pattern"] != positiveIntegerPattern {
				t.Errorf("compaction through pattern = %v", option["pattern"])
			}
			if option["name"] == "if-revision" && option["pattern"] != positiveIntegerPattern {
				t.Errorf("%s revision pattern = %v", name, option["pattern"])
			}
		}
		if expected, exists := wantQueryOptions[name]; exists {
			actual := map[string]bool{}
			for _, raw := range command["options"].([]any) {
				actual[raw.(map[string]any)["name"].(string)] = true
			}
			if len(actual) != len(expected) {
				t.Errorf("%s options = %v, want %v", name, actual, expected)
			}
			for _, option := range expected {
				if !actual[option] {
					t.Errorf("%s missing --%s", name, option)
				}
			}
		}
	}
	exit3 := catalogEnvelope.Data.ExitCodes["3"]
	if !strings.Contains(exit3, "error.code") || strings.Contains(exit3, "project_not_found") {
		t.Fatalf("exit code catalog is an incomplete error enumeration: %q", exit3)
	}
}

func TestTaskQueryOptionsRejectValuesOutsidePublishedRanges(t *testing.T) {
	a := New("test", "test")
	for _, args := range [][]string{
		{"task", "list", "--state", "draft"},
		{"task", "workstream", "list", "--state", "open"},
		{"task", "list", "--limit", "201"},
		{"task", "tree", "--depth", "21"},
		{"task", "tree", "--direction", "sideways"},
		{"task", "list", "--scope", "unknown"},
		{"task", "list", "--completion", "unknown"},
		{"task", "workstream", "plan", "show", tasks.ID(), "--at-revision", "0"},
		{"task", "validation", "record", tasks.ID(), "--result", "unknown", "--request-id", tasks.ID()},
		{"task", "workstream", "update", tasks.ID(), "--title", "x", "--if-revision", "0", "--request-id", tasks.ID()},
		{"task", "checkpoint", tasks.ID(), "--summary", "x", "--compaction-fingerprint", "bad", "--compaction-through", "1", "--request-id", tasks.ID()},
		{"task", "checkpoint", tasks.ID(), "--summary", "x", "--compaction-fingerprint", strings.Repeat("a", 64), "--compaction-through", "0", "--request-id", tasks.ID()},
	} {
		var out, diagnostic bytes.Buffer
		code := a.Run(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic})
		if code != 2 || out.Len() != 0 || !strings.Contains(diagnostic.String(), `"code":"invalid_argument"`) {
			t.Errorf("%v: exit=%d stdout=%s stderr=%s", args, code, out.String(), diagnostic.String())
		}
	}
}

func TestLongAlias(t *testing.T) {
	a := New("test", "test")
	var out, err bytes.Buffer
	if code := a.Run(context.Background(), []string{"dashboard", "start", "--help"}, IO{In: strings.NewReader(""), Out: &out, Err: &err}); code != 0 {
		t.Fatal(code, err.String())
	}
	if !strings.Contains(out.String(), "Usage: devtools dashboard") {
		t.Fatal(out.String())
	}
}
