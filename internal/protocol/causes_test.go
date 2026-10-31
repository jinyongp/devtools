package protocol

import (
	"bytes"
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
