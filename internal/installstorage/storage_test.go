package installstorage

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func TestWriteResultSupportsStrictLegacyUpdater(t *testing.T) {
	// The published v0.43.0 Windows updater rejects unknown response fields.
	var legacy struct {
		Status     string                 `json:"status"`
		Ready      bool                   `json:"ready"`
		Report     *storageupgrade.Report `json:"report,omitempty"`
		Diagnostic map[string]any         `json:"diagnostic,omitempty"`
	}
	result := Result{
		Status: "ready", Ready: true, Report: &storageupgrade.Report{Scope: "storage", Ready: true},
		Proxy: map[string]any{"changed": true}, Dashboard: map[string]any{"changed": false},
		Processes: []map[string]any{{"execution_id": "new"}}, RuntimeChanged: true,
	}
	t.Setenv(reportEnvironmentKey, "")
	var output bytes.Buffer
	if err := WriteResult(&output, result); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil || legacy.Status != "ready" || !legacy.Ready || legacy.Report == nil || !legacy.Report.Ready {
		t.Fatalf("legacy reader could not confirm the update: %+v, %v", legacy, err)
	}
	t.Setenv(reportEnvironmentKey, environmentReport)
	output.Reset()
	if err := WriteResult(&output, result); err != nil {
		t.Fatal(err)
	}
	var extended Result
	if err := json.NewDecoder(&output).Decode(&extended); err != nil || !extended.Ready || !extended.RuntimeChanged || extended.Proxy == nil || extended.Dashboard == nil || len(extended.Processes) != 1 {
		t.Fatalf("environment report lost runtime outcomes: %+v, %v", extended, err)
	}
}

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
