package protocol

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/configcontract"
)

func TestPublicDiagnosticsPreserveCausesAndPartialOutcomes(t *testing.T) {
	e := NewError("service_partial_failure", "Some commands could not be started.", 3, map[string]any{
		"path": "PRIVATE-STORAGE", "body": "PRIVATE-BODY",
		"items": []any{
			map[string]any{"command": "web", "changed": true, "status": "started", "item": map[string]any{"id": "sample-process", "token": "PRIVATE-TOKEN"}},
			map[string]any{"command": "api", "status": "failed", "condition": map[string]any{"code": "requirements_failed", "details": map[string]any{"reason": "prerequisites", "causes": []any{map[string]any{"code": "env_not_found", "reason": "not_declared", "path": "PRIVATE-STORAGE"}}}}},
		},
	})
	projected := Public(e, "services up")
	if projected.Details["effects"] != "partial" || len(projected.Details["causes"].([]any)) != 2 {
		t.Fatal(projected.Details)
	}
	var output bytes.Buffer
	if err := Failure(&output, projected); err != nil {
		t.Fatal(err)
	}
	b := output.Bytes()
	if strings.Contains(string(b), "PRIVATE-") {
		t.Fatal("private metadata escaped projection", string(b))
	}
	value, issue := configcontract.Decode(b)
	if issue != nil {
		t.Fatal(issue)
	}
	if issues := configcontract.Validate("Failure", value); len(issues) != 0 {
		t.Fatal(issues, string(b))
	}
	if !strings.Contains(string(b), `"id":"sample-process"`) {
		t.Fatal("partial success identifier lost")
	}
}
