package installstorage

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func TestCompactKeepsInstallerResultAndBlockingTarget(t *testing.T) {
	items := make([]storageupgrade.Step, 8192)
	for index := range items {
		items[index] = storageupgrade.Step{Domain: "processes", Resource: strings.Repeat("x", 128), State: "current"}
	}
	items = append(items, storageupgrade.Step{Domain: "values", Profile: "protected-project", State: "blocked", Reason: "vault_locked"})
	result := Result{Status: "pending", RuntimeChanged: true, Report: &storageupgrade.Report{
		Scope: "storage", State: "blocked", Changed: true, Effects: "committed", Items: items,
	}}
	compact := result.Compact()
	encoded, err := json.Marshal(compact)
	if err != nil || len(encoded) > 48<<10 {
		t.Fatal(len(encoded), err)
	}
	if compact.Status != "pending" || !compact.RuntimeChanged || compact.Report.Effects != "committed" || !compact.Report.Changed {
		t.Fatal(compact)
	}
	if len(compact.Report.Items) != 2 || compact.Report.Items[1].Profile != "protected-project" || compact.Report.Items[1].Reason != "vault_locked" {
		t.Fatal(compact.Report.Items)
	}
	if len(result.Report.Items) != len(items) {
		t.Fatal("full report was modified")
	}
}
