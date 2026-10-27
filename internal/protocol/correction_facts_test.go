package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/configcontract"
)

func TestCorrectionFactsSurviveProjectionAndFailureSchema(t *testing.T) {
	e := NewError("invalid_argument", "Correct this operation.", 2, map[string]any{
		"operation_index": 0,
		"body":            "PRIVATE-BODY", "context": "PRIVATE-CONTEXT",
		"facts": map[string]any{"observed_exact": false, "observed": 65, "limit": 64,
			"edit_operation": "task.update", "rule_id": "proxy.selection", "options": []string{"proxy", "no-proxy"},
			"reference_id": "00000000-0000-4000-8000-000000000001", "credential": "PRIVATE-CREDENTIAL"},
	})
	public := Public(e, "task workstream edit")
	facts := public.Details["facts"].(map[string]any)
	if facts["operation_index"] != 0 || facts["observed_exact"] != false || facts["rule_id"] != "proxy.selection" {
		t.Fatal(facts)
	}
	// A parent cause crosses JSON conversion before being projected again.
	parent := Public(NewError("requirements_failed", "A dependency failed.", 3,
		map[string]any{"causes": []any{e}}), "command run")
	cause := parent.Details["causes"].([]any)[0].(map[string]any)
	if cause["facts"].(map[string]any)["operation_index"] != json.Number("0") {
		t.Fatal(cause)
	}
	var b bytes.Buffer
	if err := Failure(&b, public); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "PRIVATE-") {
		t.Fatal("private input escaped projection")
	}
	value, issue := configcontract.Decode(b.Bytes())
	if issue != nil {
		t.Fatal(issue)
	}
	if issues := configcontract.Validate("Failure", value); len(issues) != 0 {
		t.Fatal(issues, b.String())
	}
}

func TestCorrectionFactsRejectWrongTypes(t *testing.T) {
	public := Public(NewError("invalid_argument", "Invalid input.", 2, map[string]any{"facts": map[string]any{
		"observed_exact": "false", "operation_index": -1, "rule_id": "PRIVATE value", "reference_id": "PRIVATE-CONTEXT", "options": []string{"--PRIVATE"},
	}}), "test")
	if len(public.Details["facts"].(map[string]any)) != 0 {
		t.Fatal(public.Details)
	}
}
