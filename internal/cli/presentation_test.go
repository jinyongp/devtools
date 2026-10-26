package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/cliui"
	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/protocol"
)

func presentationApp(environment presentation.Environment) *App {
	a := New("test", "commit")
	a.observeOutput = func(IO) presentation.Environment { return environment }
	return a
}

func TestInputErrorsExplainTheCorrectionWithoutRepeatedLabels(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"completion"}, []string{"Error: 'completion' requires a shell argument.", "Choose zsh, bash, fish or powershell.", "devtools help completion"}},
		{[]string{"completion", "cmd"}, []string{"'completion'", "Invalid value for argument 'shell'.", "zsh, bash, fish, powershell", "devtools help completion"}},
		{[]string{"command", "explain"}, []string{"'command explain' requires a name argument.", "devtools help command explain"}},
		{[]string{"profile", "diff"}, []string{"'profile diff'", "argument.", "devtools help profile diff"}},
		{[]string{"command", "list", "--dir"}, []string{"'command list'", "--dir requires a value", "devtools help command list"}},
		{[]string{"command", "list", "--dir", "a", "--dir", "b"}, []string{"'command list'", "Specify --dir once.", "devtools help command list"}},
		{[]string{"completion", "bash", "extra"}, []string{"'completion'", "Unexpected positional arguments", "devtools help completion"}},
		{[]string{"command", "list", "-q"}, []string{"Unknown option -q for 'command list'.", "devtools help command list"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			a := presentationApp(presentation.Environment{})
			// Exercise the Windows presentation contract on every test host.
			a.renderDocument = func(doc cliui.Document, _ cliui.Options) ([]byte, error) {
				return cliui.Render(doc, cliui.Options{Width: 120, CommandShells: []cliui.Shell{cliui.PowerShell}})
			}
			exit, out, diagnostic := invoke(t, a, "", append([]string{"--format", "text"}, tc.args...)...)
			if exit != 2 || out != "" {
				t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, diagnostic)
			}
			for _, want := range tc.want {
				if !strings.Contains(diagnostic, want) {
					t.Fatalf("missing %q: %s", want, diagnostic)
				}
			}
			for _, noise := range []string{"Command  ", "Command (", "Inspect this command's accepted", "does not match its schema"} {
				if strings.Contains(diagnostic, noise) {
					t.Fatalf("redundant diagnostic %q: %s", noise, diagnostic)
				}
			}
		})
	}
}

func TestRequiredOptionHintsComeFromTheCommandDefinition(t *testing.T) {
	a := presentationApp(presentation.Environment{})
	a.commands = append(a.commands, Command{Name: "fixture", Options: []Option{{Name: "mode", Required: true, Pattern: "^(read|write)$"}}, Run: func(context.Context, IO, Request) (any, *protocol.Error) {
		t.Fatal("invalid input reached the handler")
		return nil, nil
	}})
	for _, args := range [][]string{{"fixture"}, {"fixture", "--mode"}} {
		exit, _, diagnostic := invoke(t, a, "", append([]string{"--format", "text"}, args...)...)
		if exit != 2 || !strings.Contains(diagnostic, "Choose read or write.") || !strings.Contains(diagnostic, "devtools help fixture") {
			t.Fatal(diagnostic)
		}
	}
}

func TestInputPresentationDoesNotChangeJSONDiagnostics(t *testing.T) {
	a := presentationApp(presentation.Environment{})
	exit, _, diagnostic := invoke(t, a, "", "--format", "json", "completion")
	var response struct {
		Error struct {
			Message string `json:"message"`
			Details struct {
				Facts    map[string]any `json:"facts"`
				Remedies []struct {
					Message string `json:"message"`
				} `json:"remedies"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(diagnostic), &response) != nil || exit != 2 || response.Error.Message != "Required argument shell is missing." || response.Error.Details.Facts["allowed"] != nil || len(response.Error.Details.Remedies) != 1 || response.Error.Details.Remedies[0].Message != "Inspect this command's accepted inputs and examples." {
		t.Fatal("text hints crossed the JSON boundary", diagnostic)
	}
}

func TestPresentationDispatchAndDiscovery(t *testing.T) {
	for _, tc := range []struct {
		args []string
		exit int
		json bool
	}{
		{[]string{"--format", "text", "version"}, 0, false},
		{[]string{"--format", "text", "schema", "version"}, 2, true},
		{[]string{"--format", "text", "schema", "--help"}, 0, false},
		{[]string{"--format", "text", "schema", "command", "list", "--help"}, 2, true},
		{[]string{"--format", "text", "config", "schema", "--help"}, 0, false},
		{[]string{"--format", "text", "config", "schema", "--bogus", "--help"}, 2, true},
		{[]string{"--format", "text", "command", "list", "--bogus", "--help"}, 2, false},
		{[]string{"--format", "text", "nonexistent"}, 2, false},
		{[]string{"--format", "text", "--format", "json", "version"}, 2, true},
		{[]string{"--format=secret-invalid-value", "version"}, 2, true},
		{[]string{"--format", "json", "completion", "bash"}, 0, false},
		{[]string{"--format", "json", "version"}, 0, true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			exit, out, diagnostic := invoke(t, presentationApp(presentation.Environment{}), "", tc.args...)
			payload := out
			if exit != 0 {
				payload = diagnostic
			}
			if exit != tc.exit || json.Valid([]byte(payload)) != tc.json || strings.Contains(payload, "secret-invalid-value") {
				t.Fatalf("exit=%d stdout=%s stderr=%s", exit, out, diagnostic)
			}
		})
	}
}

func TestPresentationSelectionUsesSuppliedIO(t *testing.T) {
	for _, tc := range []struct {
		env  presentation.Environment
		args []string
		exit int
		json bool
	}{
		{presentation.Environment{Stdout: presentation.Terminal{IsTerminal: true}}, []string{"version"}, 0, false},
		{presentation.Environment{Stdout: presentation.Terminal{IsTerminal: true}, Signals: []string{"AI_AGENT"}}, []string{"version"}, 0, true},
		{presentation.Environment{}, []string{"version"}, 0, true},
		{presentation.Environment{Stdout: presentation.Terminal{IsTerminal: true}}, []string{"--format", "auto", "version"}, 0, false},
		{presentation.Environment{Stdout: presentation.Terminal{IsTerminal: true}, Signals: []string{"AI_AGENT"}}, []string{"--format", "auto", "version"}, 0, true},
		{presentation.Environment{Stderr: presentation.Terminal{IsTerminal: true}}, []string{"--format", "auto", "nonexistent"}, 2, true},
		{presentation.Environment{FormatEnv: presentation.FormatInput{Present: true, Raw: "text"}}, []string{"version"}, 0, false},
		{presentation.Environment{FormatEnv: presentation.FormatInput{Present: true, Raw: "text"}}, []string{"schema", "version"}, 0, true},
		{presentation.Environment{FormatEnv: presentation.FormatInput{Present: true, Raw: "private-invalid"}}, []string{"version"}, 2, true},
		{presentation.Environment{FormatEnv: presentation.FormatInput{Present: true, Raw: "private-invalid"}}, []string{"--format", "json", "version"}, 0, true},
	} {
		exit, out, diagnostic := invoke(t, presentationApp(tc.env), "", tc.args...)
		payload := out
		if exit != 0 {
			payload = diagnostic
		}
		if exit != tc.exit || json.Valid([]byte(payload)) != tc.json || strings.Contains(payload, "private-invalid") {
			t.Fatalf("%v: exit=%d stdout=%s stderr=%s", tc.args, exit, out, diagnostic)
		}
	}
}

func TestRootOffsetsAndInvocationOnlyFormat(t *testing.T) {
	for _, name := range [][]string{{"run"}, {"cmd", "run"}, {"command", "run"}} {
		args := append([]string{"--format", "json"}, name...)
		args = append(args, "--bogus")
		exit, _, diagnostic := invoke(t, presentationApp(presentation.Environment{}), "", args...)
		var response struct {
			Error struct {
				Details struct{ Sources []struct{ Offset int } }
			}
		}
		if json.Unmarshal([]byte(diagnostic), &response) != nil || exit != 2 || len(response.Error.Details.Sources) == 0 || response.Error.Details.Sources[0].Offset != len(args)-1 {
			t.Fatal(args, diagnostic)
		}
	}
	a := presentationApp(presentation.Environment{})
	calls := 0
	a.commands = append(a.commands, Command{Name: "fixture", OutputMode: OutputJSON, Run: func(_ context.Context, _ IO, r Request) (any, *protocol.Error) {
		calls++
		if _, exists := r.Options["format"]; exists {
			t.Fatal("presentation option entered handler request identity")
		}
		return struct {
			Public string `json:"public"`
			Secret string `json:"-"`
			Empty  string `json:"empty,omitempty"`
			Large  uint64 `json:"large"`
		}{"visible", "hidden-fixture", "", 18446744073709551615}, nil
	}})
	exit, out, diagnostic := invoke(t, a, "", "--format", "text", "fixture")
	if exit != 0 || diagnostic != "" || calls != 1 || !strings.Contains(out, "18446744073709551615") || !strings.Contains(out, "visible") || strings.Contains(out, "hidden-fixture") || strings.Contains(out, "empty") {
		t.Fatalf("exit=%d calls=%d stdout=%s stderr=%s", exit, calls, out, diagnostic)
	}
	args := []string{"--isolated-root", t.TempDir(), "--format", "json", "init"}
	exit, _, diagnostic = invoke(t, a, "", args...)
	var result struct {
		Error struct {
			Details struct{ Sources []struct{ Offset int } }
		}
	}
	if json.Unmarshal([]byte(diagnostic), &result) != nil || exit != 2 || result.Error.Details.Sources[0].Offset != len(args) {
		t.Fatal("EOF source lost", diagnostic)
	}
}

func TestTextFailurePreservesOriginalExitAndCompleteDiagnostic(t *testing.T) {
	for _, status := range []int{2, 3, 130} {
		a := presentationApp(presentation.Environment{})
		renders, calls := 0, 0
		a.renderDocument = func(cliui.Document, cliui.Options) ([]byte, error) {
			renders++
			return nil, errors.New("fixture renderer failure")
		}
		a.commands = append(a.commands, Command{Name: "fixture", OutputMode: OutputJSON, Run: func(context.Context, IO, Request) (any, *protocol.Error) {
			calls++
			return nil, protocol.NewError("fixture_error", "Original message.", status, map[string]any{"effects": "unknown", "retry": "inspect_first", "causes": []any{map[string]any{"message": "Nested cause.", "code": "nested"}}})
		}})
		exit, out, diagnostic := invoke(t, a, "", "--format", "text", "fixture")
		// Cause.message is intentionally excluded by the existing public schema;
		// the public cause code, rather than private producer data, must survive.
		if exit != status || calls != 1 || renders != 2 || out != "" || !strings.Contains(diagnostic, `"code":"nested"`) || !strings.Contains(diagnostic, "Original message.") || !strings.Contains(diagnostic, "inspect_first") || !strings.Contains(diagnostic, "Diagnostic (escaped JSON)") {
			t.Fatalf("exit=%d calls=%d renders=%d stdout=%s stderr=%s", exit, calls, renders, out, diagnostic)
		}
	}
}

type presentationFailWriter struct{ calls int }

func (w *presentationFailWriter) Write([]byte) (int, error) { w.calls++; return 0, io.ErrClosedPipe }

func TestOutputFailureStageAndNoTransportRetry(t *testing.T) {
	a := presentationApp(presentation.Environment{})
	renders, calls := 0, 0
	a.renderDocument = func(cliui.Document, cliui.Options) ([]byte, error) {
		renders++
		return nil, errors.New("render failure")
	}
	a.commands = append(a.commands, Command{Name: "fixture", OutputMode: OutputJSON, Options: []Option{{Name: "request-id"}}, Run: func(context.Context, IO, Request) (any, *protocol.Error) {
		calls++
		return map[string]any{"changed": false}, nil
	}})
	const id = "01234567-89ab-4cde-8fab-0123456789ab"
	exit, _, diagnostic := invoke(t, a, "", "--format", "text", "fixture", "--request-id", id)
	if exit != 1 || calls != 1 || renders != 2 || !strings.Contains(diagnostic, "internal_error") || !strings.Contains(diagnostic, "unknown") || !strings.Contains(diagnostic, "inspect_first") || !strings.Contains(diagnostic, id) {
		t.Fatal(exit, renders, diagnostic)
	}
	w := &presentationFailWriter{}
	var out bytes.Buffer
	exit = a.Run(context.Background(), []string{"--format", "json", "nonexistent"}, IO{In: strings.NewReader(""), Out: &out, Err: w})
	if exit != 1 || w.calls != 1 {
		t.Fatal("stderr transport was retried", exit, w.calls)
	}
	var diagnosticJSON bytes.Buffer
	failingOut := &presentationFailWriter{}
	exit = a.Run(context.Background(), []string{"--format", "json", "fixture", "--request-id", id}, IO{In: strings.NewReader(""), Out: failingOut, Err: &diagnosticJSON})
	var failure struct {
		Error struct{ Details map[string]any }
	}
	if json.Unmarshal(diagnosticJSON.Bytes(), &failure) != nil || exit != 1 {
		t.Fatal(exit, diagnosticJSON.String())
	}
	if issues := configcontract.Validate("ErrorDetails", failure.Error.Details); len(issues) != 0 {
		t.Fatal("new output error drifted from the shared diagnostic schema", issues, diagnosticJSON.String())
	}
}

func TestPresentationContextAndPublication(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := 130
		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			want = 3
		}
		cancel()
		var out, diagnostic bytes.Buffer
		exit := presentationApp(presentation.Environment{}).Run(ctx, []string{"--format", "text", "version"}, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic})
		if exit != want || out.Len() != 0 || !strings.Contains(diagnostic.String(), "Error:") {
			t.Fatal(exit, out.String(), diagnostic.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := presentationApp(presentation.Environment{})
	a.commands = append(a.commands, Command{Name: "fixture", OutputMode: OutputJSON, Run: func(context.Context, IO, Request) (any, *protocol.Error) {
		cancel()
		return map[string]any{"published": true}, nil
	}})
	var out, diagnostic bytes.Buffer
	if exit := a.Run(ctx, []string{"--format", "text", "fixture"}, IO{In: strings.NewReader(""), Out: &out, Err: &diagnostic}); exit != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "published") {
		t.Fatal(exit, out.String(), diagnostic.String())
	}
}
