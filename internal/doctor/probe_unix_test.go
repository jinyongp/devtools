//go:build !windows

package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
)

func TestProbeCacheKeysEnvironmentAndObservedExecutable(t *testing.T) {
	dir := t.TempDir()
	path, counter := filepath.Join(dir, "tool"), filepath.Join(dir, "count")
	script := "#!/bin/sh\nprintf x >> \"$COUNT_FILE\"\nprintf 'tool %s\\n' \"$PROBE_VERSION\"\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx := WithCache(context.Background())
	input := Input{Directory: dir, Environment: []string{"COUNT_FILE=" + counter, "PROBE_VERSION=1.2.3"}, Requirements: project.Requirements{Tools: map[string]project.Tool{
		"a": {Executable: path, Version: "1.2.3"}, "b": {Executable: path, Version: "1.2.3"},
	}}}
	checks := CheckRequirements(ctx, input)
	if State(checks) != "ready" {
		t.Fatal(checks)
	}
	count := func(want string) {
		t.Helper()
		b, err := os.ReadFile(counter)
		if err != nil || string(b) != want {
			t.Fatal("unexpected probe count", string(b), err)
		}
	}
	count("x")
	input.Requirements.Tools["b"] = project.Tool{Executable: path, Version: "2.0.0"}
	checks = CheckRequirements(ctx, input)
	if len(checks) != 2 || checks[0].Reason != "version_conflict" || checks[1].Reason != "version_conflict" || checks[0].Remedies[0].Effect != "config_write" {
		t.Fatal(checks)
	}
	count("x")
	delete(input.Requirements.Tools, "b")
	input.Environment[1] = "PROBE_VERSION=2.0.0"
	checks = CheckRequirements(ctx, input)
	if checks[0].Reason != "version_mismatch" {
		t.Fatal(checks)
	}
	count("xx")
	if err := os.WriteFile(path, []byte(script+"# changed executable\n"), 0700); err != nil {
		t.Fatal(err)
	}
	CheckRequirements(ctx, input)
	count("xxx")
}

func TestProbeUsesBoundSearchEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'tool %s\\n' \"$PATHEXT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	checks := CheckRequirements(context.Background(), Input{
		Directory: dir, Environment: []string{"PATHEXT=1.0.0"},
		SearchOverrides: map[string]string{"PATHEXT": "2.0.0"},
		Requirements:    project.Requirements{Tools: map[string]project.Tool{"tool": {Executable: path, Version: "2.0.0"}}},
	})
	if State(checks) != "ready" || checks[0].Facts["search_origin"] != "binding" {
		t.Fatal("probe used the parent instead of the bound search environment", checks)
	}
}

func TestProbeDoesNotTruncateInvalidVersionSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	for _, output := range []string{"1.2.3foo", "1.2.3.4", "1.2.3_custom", "1.2.3-01", "1.2.3foo 2.0.0"} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+output+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		reason, observed := probe(context.Background(), path, nil, dir, nil)
		if reason != "version_unreadable" || observed != "" {
			t.Fatalf("%q: %s %q", output, reason, observed)
		}
	}
}
