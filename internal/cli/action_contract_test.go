package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestRecoveryActionsValidateWritesAndPreserveOriginalRequest(t *testing.T) {
	a := testApp(t)
	id := "11111111-1111-4111-8111-111111111111"
	request := "22222222-2222-4222-8222-222222222222"
	scope := protocol.InvocationContext{IsolatedRoot: privateTempDir(t)}
	stop := protocol.Action{Relation: protocol.SameStoreAction, Effect: "system_write", Message: "Stop the observed run.", Intent: &protocol.CommandIntent{Command: "process stop", Arguments: []string{id}, NewRequest: true}}.Remedy(scope)
	replay := protocol.LocalAction([]string{"devtools", "process", "stop", id, "--request-id", request}, nil, "Retry the original stop.", "system_write").Remedy(scope)
	bad := protocol.LocalAction([]string{"devtools", "process", "stop", id, "--profile", "app-a", "--request-id", request}, nil, "Old invalid stop.", "system_write").Remedy(scope)
	bound := protocol.BindGuidance([]protocol.Remedy{stop, replay, bad}, scope, a.ResolveAction).([]protocol.Remedy)
	for _, r := range bound[:2] {
		if r.State != "ready" || len(r.RequiredInputs) != 0 || a.ResolveAction(r.Argv) != nil {
			t.Fatal(r)
		}
	}
	if !strings.Contains(strings.Join(bound[1].Argv, " "), request) || reflect.DeepEqual(stop.Argv, replay.Argv) {
		t.Fatal(bound)
	}
	if bound[2].State != "needs_input" || len(bound[2].Argv) != 0 || len(bound[2].InputSteps) == 0 {
		t.Fatal(bound[2])
	}
	for _, step := range bound[2].InputSteps {
		if step.State != "ready" || a.ResolveAction(step.Argv) != nil {
			t.Fatal(step)
		}
	}
	// Validation is pure: even a nonexistent execution can be parsed without
	// looking up an execution, creating storage or issuing a stop.
	if e := a.ResolveAction(bound[0].Argv); e != nil {
		t.Fatal(e)
	}
	again := protocol.BindGuidance(bound, scope, a.ResolveAction).([]protocol.Remedy)
	if !reflect.DeepEqual(bound, again) {
		t.Fatal("second binding changed request identity", bound, again)
	}
}

func TestRecoveryBindingCoversSavedMapsStaticRulesAndPrivateInputs(t *testing.T) {
	a := testApp(t)
	items := []protocol.Remedy{
		protocol.LocalAction([]string{"devtools", "vault", "unlock", "--passphrase-stdin"}, []string{"stdin"}, "Supply authentication privately.", "value_write").Remedy(protocol.InvocationContext{}),
		{Argv: []string{"devtools", "variable", "set", "KEY", "--value", "a", "--stdin"}, RequiredInputs: []string{}, Effect: "value_write", Message: "Conflicting inputs."},
		{Argv: []string{"external-tool", "--flag"}, RequiredInputs: []string{}, Effect: "read", Message: "External manual step."},
	}
	b, _ := json.Marshal(map[string]any{"report": map[string]any{"next_actions": items}})
	var saved map[string]any
	if e := json.Unmarshal(b, &saved); e != nil {
		t.Fatal(e)
	}
	scope := protocol.InvocationContext{IsolatedRoot: "/public-root"}
	result := protocol.BindGuidance(saved, scope, a.ResolveAction)
	b, _ = json.Marshal(result)
	var out struct {
		Report struct {
			Actions []protocol.Remedy `json:"next_actions"`
		} `json:"report"`
	}
	if e := json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	for _, r := range out.Report.Actions[:2] {
		if r.State != "needs_input" || len(r.Argv) != 0 || len(r.Inputs) == 0 {
			t.Fatal(r)
		}
	}
	if r := out.Report.Actions[2]; r.State != "manual" || strings.Contains(strings.Join(r.Argv, " "), "--isolated-root") {
		t.Fatal(r)
	}
}

func TestRecoveryRootPrecedesChildBoundary(t *testing.T) {
	a := testApp(t)
	r := protocol.LocalAction([]string{"devtools", "command", "add", "web", "--", "/bin/echo", "hello", "--isolated-root", "child-option"}, nil, "Save the declaration.", "config_write").Remedy(protocol.InvocationContext{IsolatedRoot: "/public-root"})
	if e := a.ResolveAction(r.Argv); e != nil {
		t.Fatal(e)
	}
	if strings.Join(r.Argv[:3], " ") != "devtools --isolated-root /public-root" {
		t.Fatal(r)
	}
	root, _ := parseRootOptions(r.Argv[1:])
	for _, c := range a.commands {
		if c.Name == "command add" {
			parsed, e := parseRequest(c, root.CommandArgs[2:])
			if e != nil || !reflect.DeepEqual(parsed.Child, []string{"/bin/echo", "hello", "--isolated-root", "child-option"}) {
				t.Fatal(parsed, e)
			}
		}
	}
}
