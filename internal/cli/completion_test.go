package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	a := New("test", "test")
	// Static script checks use a quiet helper, isolated from installed user data.
	helperDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(helperDir, "devtools"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", helperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			code, script, stderr := invoke(t, a, "", "completion", shell)
			if code != 0 || stderr != "" || !strings.Contains(script, "--profile") {
				t.Fatalf("%d %s", code, stderr)
			}
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell unavailable")
			}
			path := filepath.Join(t.TempDir(), "completion")
			if err := os.WriteFile(path, []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(binary, "-n", path).CombinedOutput(); err != nil {
				t.Fatalf("syntax: %s %v", out, err)
			}
			cases := []struct {
				words []string
				want  string
				empty bool
			}{
				{[]string{"devtools", ""}, "var", false},
				{[]string{"devtools", "var", ""}, "get", false},
				{[]string{"devtools", "variable", "set", "KEY", ""}, "--value", false},
				{[]string{"devtools", "task", "workstream", ""}, "create", false},
				{[]string{"devtools", "var", "set", "KEY", "--profile", ""}, "", true},
				{[]string{"devtools", "var", "set", "KEY", "--profile", "demo", ""}, "--value", false},
				{[]string{"devtools", "run", "--", ""}, "", true},
				{[]string{"devtools", "completion", ""}, "fish", false},
			}
			for _, tc := range cases {
				quoted := make([]string, len(tc.words))
				for i, w := range tc.words {
					quoted[i] = shellQuote(w)
				}
				var harness string
				switch shell {
				case "bash":
					harness = "source " + shellQuote(path) + "\nCOMP_WORDS=(" + strings.Join(quoted, " ") + ")\nCOMP_CWORD=${#COMP_WORDS[@]}\n((COMP_CWORD--))\n_devtools\nprintf '%s\\n' \"${COMPREPLY[@]}\""
				case "zsh":
					harness = "compdef() { :; }\n_describe() { print -rl -- $candidates; }\nsource " + shellQuote(path) + "\nwords=(" + strings.Join(quoted, " ") + ")\nCURRENT=${#words}\n_devtools"
				case "fish":
					harness = "source " + shellQuote(path) + "\ncomplete -C " + shellQuote(strings.Join(tc.words, " "))
				}
				out, err := exec.Command(binary, "-c", harness).CombinedOutput()
				if err != nil && !tc.empty {
					t.Fatalf("%v: %s %v", tc.words, out, err)
				}
				if tc.empty && strings.TrimSpace(string(out)) != "" || !tc.empty && !strings.Contains(string(out), tc.want) {
					t.Fatalf("%v: %s", tc.words, out)
				}
			}
			if shell == "zsh" {
				autoloadPath := filepath.Join(filepath.Dir(path), "_devtools")
				if err := os.WriteFile(autoloadPath, []byte(script), 0600); err != nil {
					t.Fatal(err)
				}
				harness := "fpath=(" + shellQuote(filepath.Dir(path)) + " $fpath)\nautoload -Uz _devtools\n_describe() { print -rl -- $candidates; }\nwords=(devtools var '')\nCURRENT=3\n_devtools"
				out, err := exec.Command(binary, "-c", harness).CombinedOutput()
				if err != nil || !strings.Contains(string(out), "get:") {
					t.Fatalf("autoload: %s %v", out, err)
				}
			}
		})
	}
	code, _, _ := invoke(t, a, "", "completion", "unknown")
	if code != 2 {
		t.Fatal(code)
	}
}
