package presentation

import (
	"strings"
	"testing"
)

func TestStorageReportDistinguishesUpgradeFromBlockedAndPartialEffects(t *testing.T) {
	for _, test := range []struct {
		name, state, effects, heading string
		ready, changed, upgrade       bool
	}{
		{"current", "current", "none", "Storage is current.", true, false, false},
		{"legacy", "migration_required", "none", "Storage upgrade required.", false, false, true},
		{"blocked", "blocked", "none", "Storage could not be verified.", false, false, false},
		{"partial", "blocked", "unknown", "Storage upgrade is incomplete.", false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := renderFixed(t, "doctor", map[string]any{"scope": "storage", "state": test.state, "ready": test.ready, "changed": test.changed, "effects": test.effects, "directory": "/data", "items": []any{}})
			if !strings.HasPrefix(out, test.heading) || strings.Contains(out, "--fix") != test.upgrade || !strings.Contains(out, test.effects) {
				t.Fatal(out)
			}
		})
	}
}
