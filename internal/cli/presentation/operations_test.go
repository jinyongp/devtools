package presentation

import (
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestOperationStatesDoNotClaimNewEffects(t *testing.T) {
	for _, tc := range []struct {
		command string
		data    map[string]any
		prefix  string
	}{
		{"variable set", map[string]any{"changed": true, "profile": "demo", "revision": "revision-full", "request_id": "uuid-full"}, "CHANGED"},
		{"variable set", map[string]any{"changed": false, "profile": "demo"}, "NO CHANGE"},
		{"variable set", map[string]any{"changed": true, "dry_run": true, "profile": "demo"}, "PREVIEW"},
		{"variable set", map[string]any{"changed": true, "replayed": true, "profile": "demo"}, "REPLAYED"},
		{"cleanup preview", map[string]any{"items": []any{}, "digest": "digest-full"}, "PREVIEW"},
		{"doctor", map[string]any{"ready": false, "state": "unknown", "checks": []any{}, "commands": []any{}}, "UNKNOWN"},
		{"doctor", map[string]any{"ready": false, "state": "not_ready", "checks": []any{}, "commands": []any{}}, "NOT READY"},
		{"doctor", map[string]any{"ready": false, "state": "future_state", "checks": []any{}, "commands": []any{}}, "future_state"},
	} {
		out, err := cliui.Render(publicDocument(t, tc.command, tc.data), cliui.Options{})
		if err != nil || !strings.HasPrefix(string(out), tc.prefix) {
			t.Fatal(tc.command, string(out), err)
		}
		if tc.prefix == "PREVIEW" || tc.prefix == "REPLAYED" {
			if strings.Contains(strings.Split(string(out), "\n")[0], "CHANGED") {
				t.Fatal("saved/planned change described as newly applied", string(out))
			}
		}
	}
	out, _ := cliui.Render(publicDocument(t, "variable set", map[string]any{"changed": "new-type", "effects": "unknown"}), cliui.Options{})
	if !strings.HasPrefix(string(out), "Result\n") || !strings.Contains(string(out), "new-type") {
		t.Fatal("invalid optional state was interpreted as success", string(out))
	}
}

func TestDiagnosticMessagesActionsAndUnknownFieldsStayTogether(t *testing.T) {
	data := map[string]any{"state": "not_ready", "ready": false, "checks": []any{
		map[string]any{"id": "tool", "status": "fail", "required": true, "message": "Original check message.", "target": map[string]any{"name": "node"}, "future_check": "keep-check", "remedies": []any{map[string]any{"message": "Original remedy message.", "argv": []string{"devtools", "help", "doctor"}, "required_inputs": []string{}, "effect": "read"}}},
		map[string]any{"id": "future", "status": "unrecognized", "message": "Do not infer READY from this message."},
	}, "commands": []any{}, "warnings": []any{map[string]any{"code": "warning_code", "key": "PORT", "message": "Original warning message.", "future_warning": "keep-warning"}}, "effects": "unknown", "retry": "inspect_first"}
	out, err := cliui.Render(publicDocument(t, "doctor", data), cliui.Options{CommandShells: []cliui.Shell{cliui.POSIX, cliui.Fish}})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"NOT READY", "FAIL", "WARN", "Original check message.", "Original remedy message.", "Original warning message.", "keep-check", "keep-warning", "node", "unrecognized", "inspect_first", "unknown", "devtools help doctor"} {
		if !strings.Contains(string(out), expected) {
			t.Errorf("lost %q: %s", expected, out)
		}
	}
}

func TestTaskLifecycleCompletionExecutionAndDocuments(t *testing.T) {
	data := map[string]any{"profile": "demo", "revision": 42, "valid": true, "execution_ready": false, "closable": false,
		"item":      map[string]any{"id": "id-full", "title": "Fixture task", "state": "done", "completion_status": "stale", "execution_status": "idle", "description": "first line\nsecond line", "future": 0},
		"blockers":  []any{map[string]any{"message": "Original blocker.", "target_id": "dependency-full"}},
		"documents": map[string]any{"plan": map[string]any{"body": "# Plan\nOriginal document body.", "task_ids": []string{"id-full"}, "future_document": false}, "한글": map[string]any{"body": "문서"}},
		"nodes":     []any{map[string]any{"id": "id-full", "state": "done", "completion_status": "stale", "execution_status": "idle"}},
		"edges":     []any{map[string]any{"from": "id-full", "to": "dependency-full"}}, "next_cursor": "cursor-full",
	}
	out, err := cliui.Render(publicDocument(t, "task workstream context", data), cliui.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"id-full", "done", "Completion_status", "stale", "Execution_status", "idle", "false", "42", "Original blocker.", "dependency-full", "first line\n", "second line", "# Plan\n", "Original document body.", "future_document", "cursor-full", "한글", "문서"} {
		if !strings.Contains(string(out), expected) {
			t.Errorf("lost %q: %s", expected, out)
		}
	}
	if strings.HasPrefix(string(out), "OK") {
		t.Fatal("valid was confused with executable/closable", string(out))
	}
}

func TestErrorPresentationPreservesOriginalPublicEnvelope(t *testing.T) {
	const public = `{"schema_version":1,"ok":false,"error":{"code":"partial_failure","message":"Original failure message.","new_error_field":"keep-top","details":{"target":{"name":"demo"},"checks":[{"id":"one","status":"fail","message":"Check original."}],"causes":[{"code":"nested","reason":"unknown","new_cause":"keep-cause"}],"effects":"partial","retry":"inspect_first","request_id":"uuid-original","new_diagnostic":"keep-details","remedies":[{"message":"Original next action.","argv":["devtools","help","project"],"required_inputs":[],"effect":"read"}]}}}`
	doc, err := Build("project up", []byte(public))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cliui.Render(doc, cliui.Options{CommandShells: []cliui.Shell{cliui.POSIX, cliui.Fish}})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"partial_failure", "Original failure message.", "Check original.", "nested", "keep-cause", "partial", "inspect_first", "uuid-original", "keep-details", "keep-top", "Original next action.", "devtools help project"} {
		if !strings.Contains(string(out), expected) {
			t.Errorf("lost %q: %s", expected, out)
		}
	}
	if strings.Index(string(out), "Effects") > strings.Index(string(out), "Next") {
		t.Fatal("recovery action appeared before effect/retry context", string(out))
	}
}
