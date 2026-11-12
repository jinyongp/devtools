package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestOperationIDsAreOptionalAcrossPublishedCommands(t *testing.T) {
	app := testApp(t)
	operations, selectors := 0, 0
	for _, c := range app.commands {
		for _, o := range c.Options {
			if o.Name != "request-id" {
				continue
			}
			if o.OperationID == nil {
				selectors++
				if c.Name != "values execution status" {
					t.Fatal("unclassified request ID", c.Name)
				}
				continue
			}
			operations++
			if o.Required {
				t.Fatal("operation still requires a caller-generated ID", c.Name)
			}
			for _, example := range c.Examples {
				argv := reviewedExampleTokens(t, example)
				for i := 1; i < len(argv); i++ {
					if argv[i] == "--request-id" {
						argv = append(argv[:i], argv[i+2:]...)
						break
					}
				}
				if e := app.ResolveAction(argv); e != nil {
					t.Fatalf("omitted operation ID rejected by parser/static/schema validation: %s: %v", c.Name, e)
				}
				parsed, e := parseRequest(c, argv[1+len(strings.Fields(c.Name)):])
				if e != nil || parsed.Options["request-id"] != "" {
					t.Fatalf("parsing generated an operation: %s: %v", c.Name, e)
				}
			}
		}
	}
	if operations != 80 || selectors != 1 {
		t.Fatal("operation/selector inventory changed", operations, selectors)
	}
	if e := app.ResolveAction([]string{"devtools", "values", "execution", "status"}); e == nil {
		t.Fatal("existing-execution selector became an optional operation ID")
	}
}

func TestOperationIDExecutionConditions(t *testing.T) {
	const id = "01234567-89ab-4cde-8fab-0123456789ab"
	cases := []struct {
		command string
		args    []string
		active  bool
	}{
		{"process stop", []string{id}, true},
		{"variable set", []string{"KEY", "--value", "value", "--profile", "app"}, false},
		{"variable set", []string{"KEY", "--value", "value", "--profile", "app", "--revision", strings.Repeat("a", 64)}, true},
		{"proxy setup", nil, true},
		{"proxy setup", []string{"--check"}, false},
		{"proxy setup", []string{"--check=false"}, true},
		{"proxy teardown", nil, false},
		{"proxy teardown", []string{"--apply", strings.Repeat("a", 64)}, true},
		{"task workstream edit", []string{id, "--if-revision", "1", "--file", "unused", "--dry-run"}, false},
		{"task workstream edit", []string{id, "--if-revision", "1", "--file", "unused", "--dry-run=false"}, true},
		{"expose start", []string{"web"}, false},
		{"expose start", []string{"web", "--provider", "lan", "--no-auth"}, true},
		{"values execution status", []string{"--request-id", id}, false},
	}
	for _, tc := range cases {
		t.Run(tc.command+"/"+strings.Join(tc.args, " "), func(t *testing.T) {
			app := testApp(t)
			argv := append(strings.Fields(tc.command), tc.args...)
			if e := app.ResolveAction(append([]string{"devtools"}, argv...)); e != nil {
				t.Fatal("invalid condition fixture", e)
			}
			var observed Request
			for i := range app.commands {
				if app.commands[i].Name == tc.command {
					app.commands[i].Run = func(_ context.Context, _ IO, r Request) (any, *protocol.Error) {
						observed = r
						return map[string]any{"changed": false}, nil
					}
				}
			}
			result := outputData(t, app, argv)
			requestID, present := result["request_id"]
			if present != tc.active || tc.active && requestID != observed.Options["request-id"] {
				t.Fatal("invocation and output disagree about the operation ID", result)
			}
			if _, fabricated := observed.OptionOffsets["request-id"]; tc.active && fabricated {
				t.Fatal("generated option has a fabricated argv offset")
			}
			if tc.active && !regexp.MustCompile(uuidPattern).MatchString(requestID.(string)) {
				t.Fatal("generated ID is not a UUID", requestID)
			}
			if tc.command == "values execution status" && observed.Options["request-id"] != id {
				t.Fatal("query selector was replaced")
			}
		})
	}
}

func TestGeneratedOperationIDSurvivesHandlerAndEncodingErrors(t *testing.T) {
	for _, encodingFailure := range []bool{false, true} {
		app := testApp(t)
		var id string
		calls := 0
		app.commands = append(app.commands, Command{Name: "fixture", OutputMode: OutputJSON, Options: []Option{requestIDOption(uuidPattern)}, Run: func(_ context.Context, _ IO, r Request) (any, *protocol.Error) {
			calls++
			id = r.Options["request-id"]
			if encodingFailure {
				return map[string]any{"unencodable": func() {}}, nil
			}
			return nil, protocol.NewError("fixture_failure", "Not admitted.", 3, map[string]any{"effects": "none"})
		}})
		code, out, diagnostic := invoke(t, app, "", "fixture")
		var failure struct {
			Error struct{ Details map[string]any }
		}
		if e := json.Unmarshal([]byte(diagnostic), &failure); e != nil || code == 0 || out != "" || calls != 1 || !regexp.MustCompile(uuidPattern).MatchString(id) || failure.Error.Details["target"].(map[string]any)["request_id"] != id {
			t.Fatal("failed operation lost identity or retried", code, diagnostic, e)
		}
		if encodingFailure && (failure.Error.Details["effects"] != "unknown" || failure.Error.Details["retry"] != "inspect_first") {
			t.Fatal("failure after handler success claimed no effects", diagnostic)
		}
		first := id
		invoke(t, app, "", "fixture")
		if id == first {
			t.Fatal("independent invocations reused an automatically generated ID")
		}
	}
}

func TestGeneratedOperationIDAppearsInTextSuccess(t *testing.T) {
	app := testApp(t)
	code, out, diagnostic := invoke(t, app, "", "--format", "text", "task", "add", "--profile", "app", "--title", "Text operation identity")
	match := regexp.MustCompile(`Request ID\s+([0-9a-f-]{36})`).FindStringSubmatch(out)
	if code != 0 || strings.Contains(diagnostic, "Error:") || len(match) != 2 || !regexp.MustCompile(uuidPattern).MatchString(match[1]) {
		t.Fatal("terminal success hid the generated operation ID", code, out, diagnostic)
	}
	replayed := outputData(t, app, []string{"task", "add", "--profile", "app", "--title", "Text operation identity", "--request-id", match[1]})
	if replayed["replayed"] != true {
		t.Fatal("terminal ID could not recover the same operation")
	}
}

func TestManagedProcessUsesGeneratedRequestIDsAndReplays(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='app'\n[commands.web]\nexec="+fixtureExec(t, "hold")+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	first := outputData(t, app, []string{"process", "start", "web", "--dir", root})
	execution := first["item"].(map[string]any)["id"].(string)
	requestID := first["request_id"].(string)
	t.Cleanup(func() { invoke(t, app, "", "process", "stop", execution) })
	if first["item"].(map[string]any)["state"] != "running" {
		t.Fatal("managed fixture did not start", first)
	}
	retry := outputData(t, app, []string{"process", "start", "web", "--dir", root, "--request-id", requestID})
	if retry["replayed"] != true || retry["request_id"] != requestID || retry["item"].(map[string]any)["id"] != execution {
		t.Fatal("start replay created another execution", retry)
	}
	receipt := outputData(t, app, []string{"process", "request", "show", requestID})
	if receipt["item"].(map[string]any)["request_id"] != requestID {
		t.Fatal("generated ID cannot inspect its receipt", receipt)
	}
	stopped := outputData(t, app, []string{"process", "stop", execution})
	stopID := stopped["request_id"].(string)
	if stopID == requestID || stopped["item"].(map[string]any)["state"] != "stopped" {
		t.Fatal("stop did not use its own operation ID", stopped)
	}
	retry = outputData(t, app, []string{"process", "stop", execution, "--request-id", stopID})
	if retry["replayed"] != true || retry["request_id"] != stopID {
		t.Fatal("stop replay lost its original result", retry)
	}
}
