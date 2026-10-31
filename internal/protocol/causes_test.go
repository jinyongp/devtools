package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/configcontract"
)

func TestPublicRetainsCauseRelationshipsAndResourceObservations(t *testing.T) {
	e := NewError("requirements_failed", "Cannot start web.", 3, map[string]any{
		"primary_cause_id": "c1",
		"causes": []any{map[string]any{
			"id": "c1", "code": "port_run_active", "status": "fail", "message": "Service web is already claimed.",
			"caused_by": []any{}, "remedies": []Remedy{},
			"facts": map[string]any{"service": "web", "port": 3000,
				"port_observation": map[string]any{"status": "free", "tcp_reachable": false, "reason": nil},
				"owner":            map[string]any{"status": "unavailable", "kind": nil, "pid": nil, "execution_id": nil, "command": nil, "reason": "legacy_run", "private_token": "PRIVATE-CANARY"},
			},
		}},
		"checks": []any{map[string]any{"id": "tool:node", "status": "unknown", "required": true, "reason": "binding_unresolved", "message": "PATH depends on this binding.", "target": map[string]any{"kind": "tool", "name": "node"}, "sources": []any{}, "facts": map[string]any{}, "remedies": []Remedy{}, "blocked_by": []string{"c1"}}},
	})
	for range 2 {
		e = Public(e, "command run")
		if e.Details["primary_cause_id"] != "c1" {
			t.Fatal("primary cause lost")
		}
		var output bytes.Buffer
		if err := Failure(&output, e); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "PRIVATE-CANARY") {
			t.Fatal("private owner metadata escaped")
		}
		value, issue := configcontract.Decode(output.Bytes())
		if issue != nil {
			t.Fatal(issue)
		}
		if issues := configcontract.Validate("Failure", value); len(issues) != 0 {
			t.Fatal(issues, output.String())
		}
		if !strings.Contains(output.String(), `"tcp_reachable":false`) || !strings.Contains(output.String(), `"blocked_by":["c1"]`) || !strings.Contains(output.String(), "Service web is already claimed.") {
			t.Fatal(output.String())
		}
	}
}

func TestNestedCauseNamespacesKeepReferencesDistinct(t *testing.T) {
	items := []any{}
	for _, name := range []string{"web", "api"} {
		items = append(items, map[string]any{"command": name, "status": "failed", "condition": map[string]any{"code": "requirements_failed", "message": "The command could not start.", "details": map[string]any{
			"causes": []any{map[string]any{"id": "c1", "code": "port_run_active", "status": "fail", "message": "The service is claimed.", "caused_by": []any{}, "remedies": []Remedy{}}},
			"checks": []any{map[string]any{"id": "tool:node", "status": "unknown", "required": true, "reason": "binding_unresolved", "message": "PATH is unresolved.", "target": map[string]any{"kind": "tool", "name": "node"}, "sources": []any{}, "facts": map[string]any{}, "remedies": []Remedy{}, "blocked_by": []string{"c1"}}},
		}}})
	}
	e := Public(NewError("service_partial_failure", "Some commands failed.", 3, map[string]any{"items": items}), "project up")
	var output bytes.Buffer
	if err := Failure(&output, e); err != nil {
		t.Fatal(err)
	}
	value, issue := configcontract.Decode(output.Bytes())
	if issue != nil {
		t.Fatal(issue)
	}
	if issues := configcontract.Validate("Failure", value); len(issues) > 0 {
		t.Fatal(issues, output.String())
	}
	checks := e.Details["checks"].([]any)
	first := checks[0].(map[string]any)["blocked_by"].([]any)[0]
	second := checks[1].(map[string]any)["blocked_by"].([]any)[0]
	if first == second {
		t.Fatal("independent cause namespaces were merged")
	}
	for _, mutation := range []string{"duplicate", "missing_reference", "cycle"} {
		var copy map[string]any
		if json.Unmarshal(output.Bytes(), &copy) != nil {
			t.Fatal("invalid fixture")
		}
		details := copy["error"].(map[string]any)["details"].(map[string]any)
		causes := details["causes"].([]any)
		switch mutation {
		case "duplicate":
			causes[1].(map[string]any)["id"] = causes[0].(map[string]any)["id"]
		case "missing_reference":
			details["checks"].([]any)[0].(map[string]any)["blocked_by"] = []any{"c999"}
		case "cycle":
			causes[0].(map[string]any)["caused_by"] = []any{causes[0].(map[string]any)["id"]}
		}
		if len(configcontract.Validate("Failure", copy)) == 0 {
			t.Fatal("invalid graph accepted", mutation)
		}
	}
}
