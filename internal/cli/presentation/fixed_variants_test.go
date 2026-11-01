package presentation

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestOutcomeStylesKeepOnlySemanticPrefixColored(t *testing.T) {
	strip := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, message := range []string{"Created task \"Work\".", "Preview: Format configuration.", "Stopped listener.", "No changes to task.", "Recorded result: Created task \"Work\"."} {
		doc := cliui.NewDocument(outcomeHeading(message))
		plain, _ := cliui.Render(doc, cliui.Options{})
		styled, _ := cliui.Render(doc, cliui.Options{SGR: true, Color: true})
		if strip.ReplaceAllString(string(styled), "") != string(plain) {
			t.Fatal(message, string(styled))
		}
		for _, token := range []string{"Work", "Format configuration", "Stopped", "Recorded result"} {
			if strings.Contains(string(styled), "\x1b[32m"+token) || strings.Contains(string(styled), "\x1b[36m"+token) {
				t.Fatal("colored neutral content", string(styled))
			}
		}
	}
}

func TestCreatedTaskKeepsIdentityWithoutDefaultStateDump(t *testing.T) {
	data := map[string]any{"profile": "app", "revision": 9, "changed": true, "affected_count": 1, "item": map[string]any{"id": "task-id", "kind": "task", "title": "Review output", "state": "open", "execution": "idle", "blockers": []any{}}}
	s := renderFixed(t, "task add", data)
	if strings.Count(s, "Review output") != 1 || !strings.Contains(s, "Task ID") || !strings.Contains(s, "task-id") || strings.Contains(s, "Open") || strings.Contains(s, "Idle") || strings.Contains(s, "Affected") {
		t.Fatal(s)
	}
	data["replayed"] = true
	if s = renderFixed(t, "task add", data); !strings.HasPrefix(s, "Recorded result: Created task") {
		t.Fatal(s)
	}
}

func renderFixed(t *testing.T, command string, data any) string {
	t.Helper()
	out, err := cliui.Render(publicDocument(t, command, data), cliui.Options{Width: 120, CommandShells: []cliui.Shell{cliui.POSIX}})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestExplicitAllBundleRemainsVisibleInDoctorScope(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "ok": true, "data": map[string]any{"state": "ready", "ready": true, "checks": []any{}, "profile": "app", "mode": "foreground", "commands": []any{map[string]any{"name": "dev", "state": "ready", "selection": "all", "env": ""}}}})
	for _, explicit := range []bool{false, true} {
		doc, err := BuildWithContext("doctor", b, nil, DisplayContext{BundleExplicit: explicit})
		if err != nil {
			t.Fatal(err)
		}
		out, _ := cliui.Render(doc, cliui.Options{})
		if strings.Contains(string(out), "Bundle") != explicit || strings.Contains(string(out), "_display_") {
			t.Fatal(explicit, string(out))
		}
	}
}

func TestFixedListenerInventoryUsesURLsDisabledAndCount(t *testing.T) {
	s := renderFixed(t, "proxy status", map[string]any{"items": []any{map[string]any{"name": "default", "state": "running", "port": 8080, "url": "http://127.0.0.1:8080", "https_port": 0}, map[string]any{"name": "local", "state": "stopped", "port": 9090, "url": "http://127.0.0.1:9090", "https_port": 0}}})
	for _, want := range []string{"Listener", "HTTP", "HTTPS", "Running", "Stopped", "2 listeners"} {
		if !strings.Contains(s, want) {
			t.Fatal(want, s)
		}
	}
	if strings.Count(s, "Disabled") != 2 || strings.Count(s, "http://127.0.0.1:8080") != 1 || strings.Count(s, "http://127.0.0.1:9090") != 1 {
		t.Fatal(s)
	}
}

func TestFixedSetupCheckKeepsReadOnlyIntentAndWarnings(t *testing.T) {
	s := renderFixed(t, "proxy setup", map[string]any{"item": map[string]any{"mode": "capability", "enabled": false, "intent_enabled": true, "executable_changed": true, "recovery_required": true, "bind_check": "performed_when_listener_starts"}})
	last := -1
	for _, label := range []string{"Proxy setup check", "\nSetup mode", "\nIntent", "\nCapability", "\nBind check", "recovery", "executable changed"} {
		at := strings.Index(s, label)
		if at <= last {
			t.Fatal(label, s)
		}
		last = at
	}
	if strings.Contains(s, "completed") || strings.Contains(s, "Configured") {
		t.Fatal("inspection claimed mutation", s)
	}
}

func TestFixedTeardownListsResourcesBeforeTrustConditions(t *testing.T) {
	plan := map[string]any{"digest": strings.Repeat("a", 64), "selection": map[string]any{"all": true, "remove_ca": true}, "listeners": []any{map[string]any{"name": "local"}}, "routes": []any{map[string]any{"scope": "global", "route_id": "route-id"}}, "exposures": []any{map[string]any{"listener": "local", "id": "exposure-id"}}, "ca_fingerprint": "public-fingerprint"}
	s := renderFixed(t, "proxy teardown", map[string]any{"item": plan, "dry_run": true})
	last := -1
	for _, label := range []string{"Preview:", "Listener", "Resource", "Action", "Removing the shared CA", "Digest", "No routes were changed."} {
		at := strings.Index(s, label)
		if at <= last {
			t.Fatal(label, s)
		}
		last = at
	}
	for _, name := range []string{"route-id", "exposure-id", "public-fingerprint"} {
		if !strings.Contains(s, name) {
			t.Fatal(name, s)
		}
	}
}

func TestFixedFormattingPreviewAndCheckUseSameCompactReview(t *testing.T) {
	revision := strings.Repeat("a", 64)
	report := map[string]any{"dry_run": true, "changed": false, "config_path": "/workspaces/my-app/devtools.toml", "revision": revision, "candidate_digest": strings.Repeat("b", 64), "changes": []any{map[string]any{"op": "format", "path": []any{}}}, "next_actions": []any{map[string]any{"message": "Apply the reviewed formatting.", "argv": []any{"devtools", "config", "fmt", "--dir", "/workspaces/my-app", "--revision", revision}, "required_inputs": []any{}, "effect": "config_write"}}}
	preview := renderFixed(t, "config fmt", report)
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "ok": false, "error": map[string]any{"code": "config_format_required", "message": "Configuration formatting is required.", "details": map[string]any{"report": report}}})
	doc, err := Build("config fmt", b)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cliui.Render(doc, cliui.Options{Width: 120, CommandShells: []cliui.Shell{cliui.POSIX}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.SplitN(preview, "\n", 2)[1] != strings.SplitN(string(out), "\n", 2)[1] || strings.Count(preview, revision) != 1 || strings.Contains(preview, "Candidate digest") || !strings.Contains(preview, "No files were changed.\nNext: Apply the reviewed formatting.\n  devtools config fmt") {
		t.Fatal(preview, string(out))
	}
}

func TestFixedEmptyNextAndNamedPlanReferences(t *testing.T) {
	if s := renderFixed(t, "task next", map[string]any{"profile": "my-app", "revision": 9, "item": nil}); s != "No ready task for my-app.\n" {
		t.Fatal(s)
	}
	s := renderFixed(t, "task workstream plan show", map[string]any{"profile": "", "revision": 9, "title": "Example workstream", "item": map[string]any{"body": "PLAN-BODY", "validation_ids": []any{"validation-id"}}, "task_order": []any{"task-id"}, "tasks": []any{map[string]any{"id": "task-id", "title": "Review command output"}}, "validations": []any{map[string]any{"id": "validation-id", "title": "Installed CLI acceptance"}}, "documents": map[string]any{"spec": map[string]any{"body": "UNREQUESTED-SPEC"}}})
	for _, want := range []string{"Plan for Example workstream", "Review command output", "Installed CLI acceptance"} {
		if !strings.Contains(s, want) {
			t.Fatal(want, s)
		}
	}
	if strings.Count(s, "PLAN-BODY") != 1 || strings.Count(s, "task-id") != 1 || strings.Count(s, "validation-id") != 1 || strings.Contains(s, "UNREQUESTED-SPEC") || strings.Contains(s, "Revision") {
		t.Fatal(s)
	}
}

func TestFixedBackupConflictKeepsOutputAndPreservation(t *testing.T) {
	b := []byte(`{"schema_version":1,"ok":false,"error":{"code":"backup_error","message":"Cannot create backup because the output file already exists.","details":{"reason":"output_exists","target":{"kind":"file","name":"/work/backups/app.age","profile":"app"},"effects":"none"}}}`)
	doc, err := Build("backup create", b)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cliui.Render(doc, cliui.Options{})
	if err != nil || strings.Count(string(out), "/work/backups/app.age") != 1 || !strings.Contains(string(out), "Output") || !strings.Contains(string(out), "The existing file was preserved.") {
		t.Fatal(string(out), err)
	}
}

func TestSuccessfulTerminalTaskActionsDoNotWarnAboutRevokedContext(t *testing.T) {
	for _, command := range []string{"task done", "task release"} {
		s := renderFixed(t, command, map[string]any{"profile": "app", "revision": 9, "changed": true, "context_valid": false, "item": map[string]any{"id": "task-id", "kind": "task", "title": "Work", "state": "done"}, "run": map[string]any{"id": "run-id", "state": "released"}})
		if strings.Contains(s, "not valid") || !strings.Contains(s, "run-id") {
			t.Fatal(command, s)
		}
	}
	s := renderFixed(t, "task claim", map[string]any{"profile": "app", "revision": 9, "claimed": false})
	if s != "No ready task for app.\n" {
		t.Fatal(s)
	}
}

func TestPreviewVariantsRetainMembersMappingsAndRestoreTargets(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "ok": true, "data": map[string]any{"profile": "app", "dry_run": true, "changed": false, "bundle": "web"}})
	doc, err := BuildWithContext("bundle set", b, nil, DisplayContext{Target: "web", MembersKnown: true, MemberNames: []string{"API_URL", "TOKEN"}})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := cliui.Render(doc, cliui.Options{})
	for _, want := range []string{"Preview: Set bundle \"web\".", "API_URL", "TOKEN", "No values were changed."} {
		if !strings.Contains(string(out), want) {
			t.Fatal(want, string(out))
		}
	}
	s := renderFixed(t, "proxy hosts preview", map[string]any{"changed": true, "digest": strings.Repeat("a", 64), "mappings": []any{map[string]any{"host": "api.test", "address": "127.0.0.1"}}})
	if !strings.Contains(s, "api.test") || !strings.Contains(s, "127.0.0.1") || !strings.Contains(s, "No files were changed.") {
		t.Fatal(s)
	}
	s = renderFixed(t, "backup restore", map[string]any{"dry_run": true, "changed": false, "targets": []any{map[string]any{"source": "old-app", "profile": "new-app", "exists": false}}})
	for _, want := range []string{"Source", "Target", "old-app", "new-app", "New"} {
		if !strings.Contains(s, want) {
			t.Fatal(want, s)
		}
	}
	if strings.Contains(s, "Unavailable") {
		t.Fatal("known restore target hidden", s)
	}
}
