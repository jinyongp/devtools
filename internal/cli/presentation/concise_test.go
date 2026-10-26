package presentation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestCommandInventoryDoesNotGrowASecondList(t *testing.T) {
	var items []any
	for i := 0; i < 27; i++ {
		items = append(items, map[string]any{"name": fmt.Sprintf("command-%02d", i), "exec": []string{"./scripts/project", "python", "-B", "-m", "unittest", "discover", "-s", "perf", "-p", "test_run.py"}, "env": "", "inject": false, "selection": "all", "serve": []string{}, "output": map[string]any{"mode": "auto"}})
	}
	doc := publicDocument(t, "command list", map[string]any{"profile": "fixture", "items": items})
	for _, width := range []int{40, 80, 120} {
		output, err := cliui.Render(doc, cliui.Options{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		text := string(output)
		if lines := strings.Count(text, "\n"); lines > 30 {
			t.Fatalf("27 commands expanded to %d lines at width %d: %s", lines, width, text)
		}
		for i := 0; i < 27; i++ {
			if strings.Count(text, fmt.Sprintf("command-%02d", i)) != 1 {
				t.Fatal("command duplicated or missing", text)
			}
		}
		if strings.Count(text, "test_run.py") != 27 || strings.Contains(text, "argv tokens") || strings.Contains(text, "Details") {
			t.Fatal("executable content replaced by counts or metadata", text)
		}
	}
}

func TestInventoryShowsNondefaultOperationalSettings(t *testing.T) {
	data := map[string]any{"profile": "fixture", "items": []any{map[string]any{
		"name": "web", "exec": []string{"node", "server.js"}, "env": "local", "inject": true,
		"selection": "web", "serve": []string{"http"}, "output": map[string]any{"mode": "raw"}, "values_update": "restart",
	}}}
	output, err := cliui.Render(publicDocument(t, "command list", data), cliui.Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{"node server.js", "values: local", "bundle: web", "serves: http", "unmasked output", "restart on value changes"} {
		if !strings.Contains(string(output), need) {
			t.Fatalf("operational setting hidden: %q: %s", need, output)
		}
	}
}

func TestRecoveryNotesDoNotLoseRetryIdentity(t *testing.T) {
	data := map[string]any{"effects": "unknown", "retry": "same_request", "target": map[string]any{"request_id": "same-uuid"}}
	output, err := cliui.Render(cliui.NewDocument(recoveryNotes(data)...), cliui.Options{})
	if err != nil || !strings.Contains(string(output), "Changes may have been applied.") || !strings.Contains(string(output), "same request ID: same-uuid") || strings.Contains(string(output), "same_request") {
		t.Fatal(string(output), err)
	}
}

func TestReadinessExplainsFailureWithoutReportMetadata(t *testing.T) {
	data := map[string]any{"id": "process-id", "ready_configured": true, "readiness": map[string]any{
		"ready": false, "checked_at": "2026-10-26T00:00:00Z", "reason": "probe_failed", "exit_code": 7,
	}}
	output, err := cliui.Render(publicDocument(t, "process check", data), cliui.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"NOT READY", "process-id", "The readiness check failed.", "exit code: 7"} {
		if !strings.Contains(string(output), expected) {
			t.Fatalf("lost %q: %s", expected, output)
		}
	}
	for _, noise := range []string{"Details", "checked_at", "ready_configured", "probe_failed"} {
		if strings.Contains(string(output), noise) {
			t.Fatalf("raw report metadata %q: %s", noise, output)
		}
	}
}
