package presentation

import (
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
	"github.com/jinyongp/devtools/internal/diagnostics"
	"github.com/jinyongp/devtools/internal/profiles"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestIntegratedDiagnosticsShowSubsystemStatesWithoutSuccessfulRecords(t *testing.T) {
	report := diagnostics.Report{Ready: true, Profiles: diagnostics.Profiles{Section: diagnostics.Section{Status: "pass"}}, Proxy: diagnostics.Proxy{Section: diagnostics.Section{Status: "pass"}, State: "stopped"}, Backup: diagnostics.Backup{Section: diagnostics.Section{Status: "pass"}}, Issues: []diagnostics.Issue{}}
	out, err := cliui.Render(publicDocument(t, "diagnostics", report), cliui.Options{Width: 80})
	s := string(out)
	for _, want := range []string{"Diagnostics: Ready.", "Subsystem", "State", "Profiles", "Proxy", "Backup"} {
		if err != nil || !strings.Contains(s, want) {
			t.Fatalf("missing %q: %s (%v)", want, s, err)
		}
	}
	if strings.Contains(s, "Not ready") || strings.Contains(s, "Paths") || strings.Count(s, "\n") > 20 {
		t.Fatal("stopped/unconfigured state became failure or report expanded", s)
	}
}

func TestIntegratedProfileDiffIdentifiesLocationAndScopeChanges(t *testing.T) {
	alias := "worktree"
	diff := profiles.Diff{Left: "before", Right: "after", Different: true,
		Variables: []profiles.ValueChange{{Key: "API", Action: "changed", Left: &profiles.ValueScope{Key: "API", Common: true}, Right: &profiles.ValueScope{Key: "API", Envs: []string{"local"}}}},
		Instances: []profiles.InstanceChange{{Action: "removed", Left: &profiles.InstanceIdentity{Alias: &alias, Directory: "/work/old"}}, {Action: "added", Right: &profiles.InstanceIdentity{Directory: "/work/new"}}},
	}
	out, err := cliui.Render(publicDocument(t, "profile diff", diff), cliui.Options{Width: 80})
	for _, want := range []string{"API", "common", "local", "/work/old", "/work/new", "worktree"} {
		if err != nil || !strings.Contains(string(out), want) {
			t.Fatalf("missing %q: %s (%v)", want, out, err)
		}
	}
}

func TestDoctorKeepsBundleAndDistinguishesCommandFailureTargets(t *testing.T) {
	commands := []any{}
	for _, name := range []string{"web", "api"} {
		commands = append(commands, map[string]any{"name": name, "env": "", "selection": "none", "state": "not_ready", "checks": []any{map[string]any{"status": "fail", "reason": "not_found", "message": "The executable is missing.", "target": map[string]any{"name": "node"}, "facts": map[string]any{}, "remedies": []any{}}}})
	}
	out, _ := cliui.Render(publicDocument(t, "doctor", map[string]any{"state": "not_ready", "ready": false, "checks": []any{}, "commands": commands[:1]}), cliui.Options{})
	if !strings.Contains(string(out), "Bundle") || !strings.Contains(string(out), "none") {
		t.Fatal("effective bundle hidden", string(out))
	}
	out, _ = cliui.Render(publicDocument(t, "doctor", map[string]any{"state": "not_ready", "ready": false, "checks": []any{}, "commands": commands}), cliui.Options{})
	if !strings.Contains(string(out), "web, api") {
		t.Fatal("shared cause lost affected command names", string(out))
	}
}

func TestScopeAndVersionOrderAndEquivalentActionsFollowContract(t *testing.T) {
	out, _ := cliui.Render(publicDocument(t, "version", map[string]any{"version": "v-test", "commit": "commit-id", "protocol_version": 22}), cliui.Options{})
	if strings.Index(string(out), "Commit") > strings.Index(string(out), "Protocol") {
		t.Fatal("version fields reordered", string(out))
	}
	out, _ = cliui.Render(publicDocument(t, "variable get", map[string]any{"profile": "app", "env": "", "directory": "/work/app", "value": "actual-value", "metadata": map[string]any{"key": "MODE", "source": "common"}, "integrity_verified": false}), cliui.Options{})
	s := string(out)
	if strings.Index(s, "Profile") > strings.Index(s, "Directory") || strings.Index(s, "Directory") > strings.Index(s, "Environment") || strings.Index(s, "Environment") > strings.Index(s, "Metadata only") {
		t.Fatal("scope changed with integrity observation", s)
	}
	actions := []any{}
	for _, message := range []string{"Read the page.", "Continue the same query."} {
		actions = append(actions, map[string]any{"message": message, "argv": []any{"devtools", "task", "list"}, "required_inputs": []any{}, "effect": "read"})
	}
	blocks, _ := actionBlocks(actions)
	out, _ = cliui.Render(cliui.NewDocument(blocks...), cliui.Options{CommandShells: []cliui.Shell{cliui.POSIX}})
	if strings.Count(string(out), "devtools task list") != 1 {
		t.Fatal("same action repeated", string(out))
	}
}

func TestValidationProducerKeepsRecordedOutcomeInListAndMutation(t *testing.T) {
	state := &tasks.State{}
	for _, result := range []string{"pass", "fail", "blocked"} {
		item := &tasks.Item{ID: "validation-id", Kind: "validation", State: "open", Title: "Check", Props: tasks.Object{"required": true, "records": []any{map[string]any{"record_id": "record-id", "result": result, "applicable": false, "evidence": []any{"PRIVATE-RECORD-BODY"}}}}}
		view := state.View(item)
		data := map[string]any{"profile": "app", "revision": 1, "items": []any{view}}
		out, err := cliui.Render(publicDocument(t, "task validation list", data), cliui.Options{})
		if err != nil || !strings.Contains(string(out), "Recorded "+result+" (not applicable)") || strings.Contains(string(out), "Not recorded") || strings.Contains(string(out), "PRIVATE-RECORD-BODY") {
			t.Fatal(string(out), err)
		}
		out, _ = cliui.Render(publicDocument(t, "task validation record", map[string]any{"profile": "app", "revision": 1, "item": view, "record_id": "record-id", "changed": true}), cliui.Options{})
		if !strings.Contains(string(out), "Recorded validation result: "+result+".") {
			t.Fatal("record outcome hidden", string(out))
		}
	}
}

func TestDetailSectionsKeepContractOrderWithoutExtensionTrees(t *testing.T) {
	data := map[string]any{"profile": "app", "directory": "/work/app", "revision": "revision-id", "future_root": "ROOT-NOISE", "item": map[string]any{"name": "web", "exec": []string{"node", "server.js"}, "env": "", "inject": true, "serve": []string{"http"}, "bind": map[string]any{"PORT": "http"}, "requirements": map[string]any{"tools": map[string]any{"node": "24.x"}}, "output": map[string]any{"mode": "raw"}, "future_item": "ITEM-NOISE"}}
	out, err := cliui.Render(publicDocument(t, "command explain", data), cliui.Options{Width: 80})
	s := string(out)
	last := -1
	for _, label := range []string{"Profile", "Directory", "Environment", "Exec", "Services", "Bindings", "Requirements", "Output policy"} {
		at := strings.Index(s, label)
		if err != nil || at <= last {
			t.Fatalf("field %q missing or reordered: %s (%v)", label, s, err)
		}
		last = at
	}
	if strings.Contains(s, "ROOT-NOISE") || strings.Contains(s, "ITEM-NOISE") {
		t.Fatal("extension fields expanded text", s)
	}
	out, _ = cliui.Render(publicDocument(t, "vault status", map[string]any{"item": map[string]any{"state": "locked_manual", "backend": "passphrase", "vault_id": "INTERNAL-ID"}}), cliui.Options{})
	if !strings.Contains(string(out), "Manually locked") || strings.Contains(string(out), "Unrecognized") || strings.Contains(string(out), "INTERNAL-ID") {
		t.Fatal(string(out))
	}
}
