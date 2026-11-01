package presentation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestClaimFailureHasOneCauseStablePortOwnerAndOneAction(t *testing.T) {
	for _, verified := range []bool{true, false} {
		owner := map[string]any{"status": "unavailable", "kind": nil, "pid": nil, "execution_id": nil, "command": nil, "reason": "legacy_run"}
		if verified {
			owner = map[string]any{"status": "verified", "kind": "foreground", "pid": 42, "execution_id": nil, "command": "dev:web", "reason": nil}
		}
		action := map[string]any{"message": "Stop the existing run in its original terminal, then retry.", "argv": []any{}, "required_inputs": []any{}, "effect": "read"}
		root := map[string]any{"id": "c1", "code": "port_run_active", "status": "fail", "message": "Service \"web\" is claimed by another run.", "caused_by": []any{}, "facts": map[string]any{"service": "web", "port": 3000, "port_observation": map[string]any{"status": "free", "tcp_reachable": false, "reason": nil}, "owner": owner}, "remedies": []any{action}}
		public, _ := json.Marshal(map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": "requirements_failed", "message": "Cannot start \"dev:web\".", "details": map[string]any{"operation": "command run", "target": map[string]any{"kind": "command", "name": "dev:web", "profile": "app", "directory": "/work/app", "env": "", "bundle": "all"}, "facts": map[string]any{}, "primary_cause_id": "c1", "causes": []any{root}, "checks": []any{map[string]any{"id": "tool:node", "status": "pass", "message": "Node is available.", "sources": []any{map[string]any{"kind": "config", "path": []any{"tools", "node"}, "line": 292, "column": 1}}}}, "remedies": []any{action}, "effects": "none"}}})
		doc, err := Build("command run", public)
		if err != nil {
			t.Fatal(err)
		}
		out, err := cliui.Render(doc, cliui.Options{Width: 80})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if strings.Count(s, "Port ") != 1 || strings.Count(s, "Owner ") != 1 || strings.Index(s, "Port ") > strings.Index(s, "Owner ") || strings.Count(s, "Next:") != 1 {
			t.Fatal(s)
		}
		for _, bad := range []string{"doctor", "environment_unresolved", "Node is available", "tools.node", "prerequisites failed:"} {
			if strings.Contains(s, bad) {
				t.Fatal("redundant or misleading diagnostic", s)
			}
		}
		if !strings.Contains(s, "3000 (no TCP listener)") || !strings.Contains(s, "Environment  common") || strings.Contains(s, "Bundle ") {
			t.Fatal(s)
		}
	}
}

func TestMutationPreviewDoesNotClaimChangesAndTaskBodyAppearsOnce(t *testing.T) {
	preview := publicDocument(t, "config fmt", map[string]any{"dry_run": true, "changed": false, "config_path": "/work/app/devtools.toml", "revision": strings.Repeat("a", 64), "changes": []any{map[string]any{"op": "format", "path": []any{}}}})
	out, err := cliui.Render(preview, cliui.Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "Preview: Format configuration.") || !strings.Contains(s, "No files were changed.") || strings.Contains(s, "NO CHANGE") || strings.Contains(s, "CHANGED") {
		t.Fatal(s)
	}
	body := "Public plan body sentinel"
	plan := map[string]any{"body": body, "task_ids": []any{"task-id"}}
	doc := publicDocument(t, "task workstream plan show", map[string]any{"profile": "app", "revision": 17, "item": plan, "documents": map[string]any{"plan": plan, "spec": map[string]any{"body": "UNREQUESTED-SPEC"}}, "tasks": []any{map[string]any{"description": "UNREQUESTED-DESCRIPTION"}}, "task_order": []any{"task-id"}})
	out, err = cliui.Render(doc, cliui.Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	s = string(out)
	if strings.Count(s, body) != 1 || strings.Contains(s, "UNREQUESTED") || !strings.Contains(s, "task-id") {
		t.Fatal(s)
	}
}

func TestUnverifiedDoctorRetainsReasonSpecificRecoveryOnce(t *testing.T) {
	action := map[string]any{"message": "The diagnosis exceeded its overall timeout. Review the timeout before retrying.", "argv": []any{}, "required_inputs": []any{}, "effect": "read"}
	checks := []any{}
	for _, name := range []string{"node", "pnpm"} {
		checks = append(checks, map[string]any{"id": "tool:" + name, "status": "unknown", "reason": "deadline_exceeded", "facts": map[string]any{"executable": name}, "remedies": []any{action}})
	}
	doc := publicDocument(t, "doctor", map[string]any{"ready": false, "state": "unknown", "checks": []any{}, "commands": []any{map[string]any{"name": "dev", "state": "unknown", "checks": checks}}})
	out, err := cliui.Render(doc, cliui.Options{Width: 120})
	if err != nil || !strings.Contains(string(out), "Unverified checks: node, pnpm") || strings.Count(string(out), "overall timeout") != 1 || strings.Count(string(out), "Next:") != 1 {
		t.Fatal(string(out), err)
	}
}

func TestIndependentUnknownCauseDoesNotRepeatAsUnverifiedCheck(t *testing.T) {
	target := map[string]any{"kind": "tool", "name": "node"}
	facts := map[string]any{"executable": "node"}
	check := map[string]any{"id": "tool:node", "status": "unknown", "reason": "timeout", "target": target, "facts": facts, "blocked_by": []any{}}
	cause := map[string]any{"id": "c1", "status": "unknown", "code": "timeout", "message": "The Node version probe timed out.", "target": target, "facts": facts, "caused_by": []any{}}
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": "requirements_failed", "message": "Requirements not met.", "details": map[string]any{"causes": []any{cause}, "checks": []any{check}}}})
	doc, err := Build("command run", b)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cliui.Render(doc, cliui.Options{})
	if err != nil || !strings.Contains(string(out), "The Node version probe timed out.") || strings.Contains(string(out), "Unverified checks:") {
		t.Fatal(string(out), err)
	}
}
