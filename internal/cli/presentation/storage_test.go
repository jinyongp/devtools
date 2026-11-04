package presentation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
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

func TestStorageFailureShowsBlockedTargetsBeforeSuccessfulInventory(t *testing.T) {
	items := []any{}
	for index := range 200 {
		items = append(items, map[string]any{"domain": "values", "profile": fmt.Sprintf("current-%03d", index), "source": "values-v1", "target": "values-v1", "state": "current"})
	}
	items = append(items,
		map[string]any{"domain": "values", "profile": "review-warning", "source": "values-v1", "target": "values-v1", "state": "current", "reason": "metadata_warning"},
		map[string]any{"domain": "tasks", "profile": "upgrade-me", "source": "journal-v2", "target": "task-v3", "state": "migration_required"},
		map[string]any{"domain": "tasks", "profile": "blocked-profile", "source": "task-v3", "target": "task-v3", "state": "blocked", "reason": "deadline_exceeded"},
	)
	out := renderFixed(t, "doctor", map[string]any{"scope": "storage", "state": "blocked", "ready": false, "effects": "none", "directory": "/data", "items": items})
	if strings.Contains(out, "current-") || !strings.Contains(out, "200 storage items are current.") || !strings.Contains(out, "metadata warning") {
		t.Fatal("successful inventory obscured the failure or a meaningful diagnostic was hidden", out)
	}
	blocked, upgrade, warning := strings.Index(out, "blocked-profile"), strings.Index(out, "upgrade-me"), strings.Index(out, "review-warning")
	if blocked < 0 || upgrade < blocked || warning < blocked {
		t.Fatal("the blocking target must be visible before other inventory", out)
	}
	current := renderFixed(t, "doctor", map[string]any{"scope": "storage", "state": "current", "ready": true, "effects": "none", "directory": "/data", "items": items[:1]})
	if !strings.Contains(current, "current-000") {
		t.Fatal("a successful storage inventory lost its requested detail", current)
	}
}

func TestStorageFailureUsesOnlyTheManagedStorageScope(t *testing.T) {
	doc, err := Build("doctor", []byte(`{"schema_version":1,"ok":false,"error":{"code":"requirements_failed","message":"Managed storage could not be verified.","details":{"target":{"directory":".","profile":"irrelevant-project"},"report":{"scope":"storage","directory":"/data/devtools","state":"blocked","ready":false,"effects":"none","items":[]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := cliui.Render(doc, cliui.Options{Width: 120})
	if err != nil || strings.Count(string(out), "Directory") != 1 || !strings.Contains(string(out), "/data/devtools") || strings.Contains(string(out), "irrelevant-project") {
		t.Fatal("project scope obscured the actual managed storage target", string(out), err)
	}
}
