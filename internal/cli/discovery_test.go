package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnknownHelpCommand(t *testing.T) {
	a := New("test", "test")
	for _, args := range [][]string{{"get", "--help"}, {"get", "-h"}, {"help", "get"}, {"var", "missing", "--help"}} {
		code, out, stderr := invoke(t, a, "", args...)
		if code != 2 || out != "" || stderr != "Unknown command. Run devtools --help.\n" {
			t.Fatalf("%v: %d %q %q", args, code, out, stderr)
		}
	}
	for _, args := range [][]string{{"get"}, {"schema", "get"}} {
		code, out, stderr := invoke(t, a, "", args...)
		if code != 2 || out != "" || !json.Valid([]byte(stderr)) || !strings.Contains(stderr, `"code":"invalid_argument"`) {
			t.Fatalf("%v: %d %q %q", args, code, out, stderr)
		}
	}
}

func TestScopedDiscovery(t *testing.T) {
	a := New("test", "test")
	for _, args := range [][]string{nil, {"--help"}, {"task", "--help"}, {"task", "workstream", "--help"}, {"var", "set", "--help"}} {
		code, out, err := invoke(t, a, "", args...)
		if code != 0 || err != "" || !strings.Contains(out, "Usage:") || strings.Contains(out, "input_schema") {
			t.Fatalf("%v: %d %s %s", args, code, out, err)
		}
	}
	_, index, _ := invoke(t, a, "", "schema")
	_, scoped, _ := invoke(t, a, "", "schema", "var", "get")
	_, full, _ := invoke(t, a, "", "schema", "--all")
	if len(index) > 2000 || len(scoped) >= len(full)/5 || !strings.Contains(scoped, "input_schema") || strings.Contains(scoped, `"options":`) {
		t.Fatalf("discovery sizes %d %d %d", len(index), len(scoped), len(full))
	}
	code, _, _ := invoke(t, a, "", "schema", "missing")
	if code != 2 {
		t.Fatal(code)
	}
}
