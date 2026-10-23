package doctor

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func TestMetadataAndVersionChecks(t *testing.T) {
	dir := privateTempDir(t)
	tool := doctorVersionFixture(t, dir)
	store := values.Store{Directory: filepath.Join(dir, "profiles"), Profile: "test"}
	_, e := store.Update(context.Background(), func(s *values.State) (bool, *protocol.Error) {
		if _, e := s.CreateEnv("dev"); e != nil {
			return false, e
		}
		if _, e := s.Set(values.Variable, "PORT", "", "3000"); e != nil {
			return false, e
		}
		return s.Set(values.Secret, "TOKEN", "dev", "fixture-secret")
	})
	if e != nil {
		t.Fatal(e)
	}
	state, e := store.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	in := Input{Directory: dir, Env: "dev", State: state, Inject: true, Requirements: project.Requirements{Tools: map[string]project.Tool{"tool": {Executable: "./" + filepath.Base(tool), Version: "1.2.3"}}, Vars: []string{"PORT"}, Secs: []string{"TOKEN"}}}
	checks := CheckRequirements(context.Background(), in)
	for _, c := range checks {
		if c.Status != "pass" {
			t.Fatal(c)
		}
	}
	raw, _ := json.Marshal(checks)
	if strings.Contains(string(raw), "fixture-secret") || strings.Contains(string(raw), "3000") {
		t.Fatal("value or tool output leaked")
	}
	in.Env = ""
	checks = CheckRequirements(context.Background(), in)
	if checks[len(checks)-1].Status != "fail" {
		t.Fatal("env-only secret treated as common")
	}
	in.Requirements.Tools["tool"] = project.Tool{Executable: "./" + filepath.Base(tool), Version: "1.2.30"}
	checks = CheckRequirements(context.Background(), in)
	if checks[0].Status != "fail" {
		t.Fatal("prefix version accepted")
	}
}
func TestProbeFailureAndCancellation(t *testing.T) {
	dir := privateTempDir(t)
	tool := doctorProbeFixture(t, dir, false)
	in := Input{Directory: dir, Requirements: project.Requirements{Tools: map[string]project.Tool{"tool": {Executable: tool, Version: "1.2.3"}}}}
	checks := CheckRequirements(context.Background(), in)
	raw, _ := json.Marshal(checks)
	if checks[0].Status != "fail" || strings.Contains(string(raw), "fixture-secret") {
		t.Fatal(checks)
	}
	in.Requirements.Tools["tool"] = project.Tool{Executable: doctorProbeFixture(t, dir, true), Version: "1.2.3"}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	checks = CheckRequirements(ctx, in)
	if checks[0].Status != "unknown" || checks[0].Reason != "deadline_exceeded" || time.Since(start) > 4*time.Second {
		t.Fatal("probe did not cancel", checks)
	}
}
func TestBoundedProbeOutput(t *testing.T) {
	b := &limitedOutput{}
	input := strings.Repeat("x", 32<<10)
	n, e := b.Write([]byte(input))
	if e != nil || n != len(input) || b.data.Len() != 16<<10 || !b.truncated {
		t.Fatal("output bound")
	}
}

func TestFailureRemedyPreservesInjectionSelection(t *testing.T) {
	checks := []Check{NewCheck("command_executable", "fail", "not_found", "The executable is missing.")}
	for _, inject := range []bool{false, true} {
		e := FailureFor(checks, protocol.InvocationContext{Kind: "command", Name: "web", Directory: "/project", Profile: "sample", Env: "", Bundle: "none", Mode: "foreground", Inject: inject, ProfileKnown: true, EnvKnown: true, BundleKnown: true})
		remedy := e.Details["remedies"].([]protocol.Remedy)[0]
		hasEnv := false
		for _, arg := range remedy.Argv {
			hasEnv = hasEnv || arg == "--env"
		}
		if hasEnv != inject {
			t.Fatal("remedy changes injection semantics", remedy.Argv)
		}
	}
}
