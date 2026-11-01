package presentation

import (
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
	"github.com/jinyongp/devtools/internal/diagnostics"
	"github.com/jinyongp/devtools/internal/profiles"
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
