package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObservationDiagnosticKeepsSafeCauseOnly(t *testing.T) {
	e := NewError("storage_error", "private-message-sentinel", 3, map[string]any{
		"domain": "values", "operation": "open_profile", "resource": "private_file", "reason": "access_denied", "os_error_domain": "errno", "os_error": 13,
		"state": "initialization_required", "effects": "unknown", "retry": "after_dependency_change", "phase": "prepare",
		"path": "/private-path-sentinel", "facts": map[string]any{"secret": "private-value-sentinel"}, "report": "private-report-sentinel",
	})
	diagnostic := ObservationDiagnostic(e)
	raw, err := json.Marshal(diagnostic)
	if err != nil || strings.Contains(string(raw), "sentinel") {
		t.Fatal("private error copied", err)
	}
	details := diagnostic["details"].(map[string]any)
	if details["state"] != "initialization_required" || details["reason"] != "access_denied" || details["os_error"] != 13 || details["effects"] != "unknown" {
		t.Fatal(diagnostic)
	}
	details["reason"] = "changed"
	if e.Details["reason"] != "access_denied" {
		t.Fatal("projection aliases original error")
	}
}
