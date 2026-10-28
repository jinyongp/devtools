package presentation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestLaunchFailureTextDistinguishesOldNewAndHistoricalOutage(t *testing.T) {
	const old = "11111111-1111-4111-8111-111111111111"
	const next = "22222222-2222-4222-8222-222222222222"
	for _, unknown := range []bool{false, true} {
		code, effects := "execution_failed", "partial"
		if unknown {
			code, effects = "launch_outcome_unknown", "unknown"
		}
		body, _ := json.Marshal(map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": code, "message": "Inspect the saved launch result.", "details": map[string]any{
			"operation": "process restart", "phase": "observe", "effects": effects, "retry": "inspect_first",
			"target": map[string]any{"kind": "process", "name": next, "id": next},
			"facts":  map[string]any{"previous_id": old, "outage": true, "replayed": true},
		}}})
		doc, e := Build("process restart", body)
		if e != nil {
			t.Fatal(e)
		}
		rendered, e := cliui.Render(doc, cliui.Options{Width: 80})
		if e != nil {
			t.Fatal(e)
		}
		text := strings.Join(strings.Fields(string(rendered)), " ")
		for _, want := range []string{"Previous execution", old, "New execution", next, "Saved result replayed", "Check current state separately"} {
			if !strings.Contains(text, want) {
				t.Fatalf("lost %q: %s", want, text)
			}
		}
		if unknown {
			if strings.Contains(text, "service gap") || strings.Contains(text, "did not start") || !strings.Contains(text, "replacement outcome is unknown") {
				t.Fatal("unknown launch became a confirmed gap", text)
			}
		} else if !strings.Contains(text, "Recorded service gap") {
			t.Fatal("lost historical outage", text)
		}
	}
}
