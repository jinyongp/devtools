package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActionPreservesOnlyItsDeclaredStore(t *testing.T) {
	c := InvocationContext{IsolatedRoot: "/chosen", Profile: "p", Env: "", ProfileKnown: true, EnvKnown: true}
	a := LocalAction([]string{"devtools", "vault", "status"}, nil, "Inspect protection.", "read")
	r := a.Remedy(c)
	if strings.Join(r.Argv, " ") != "devtools vault status --isolated-root /chosen" {
		t.Fatal(r)
	}
	a.Relation = DestinationAction
	if len(a.Remedy(c).Argv) != 3 {
		t.Fatal("destination inherited caller root")
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "Profile") || strings.Contains(string(b), "Relation") {
		t.Fatal(string(b))
	}
}

func TestBindGuidancePreservesNestedActionsAndDestination(t *testing.T) {
	type check struct{ Remedies []Remedy }
	type report struct{ Checks []check }
	local := LocalAction([]string{"devtools", "vault", "status"}, nil, "Inspect.", "read").Remedy(InvocationContext{})
	destination := Action{Argv: []string{"devtools", "profile", "transfer", "prepare"}, Message: "On destination.", Effect: "value_write", Relation: DestinationAction}.Remedy(InvocationContext{})
	value := map[string]any{"report": report{Checks: []check{{Remedies: []Remedy{local, destination}}}}}
	bound := BindGuidance(value, InvocationContext{IsolatedRoot: "/B"})
	b, _ := json.Marshal(bound)
	if strings.Count(string(b), "--isolated-root") != 1 {
		t.Fatal(string(b))
	}
	original, _ := json.Marshal(value)
	if strings.Contains(string(original), "/B") {
		t.Fatal("binding mutated shared domain guidance")
	}
}
