//go:build !windows

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	a := New("test", "test")
	// Shells call the same real candidate engine as the CLI, isolated from user data.
	helperDir := privateTempDir(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"devtools", "dvt"} {
		if err := os.WriteFile(filepath.Join(helperDir, name), []byte("#!/bin/sh\nexec "+shellQuote(executable)+" -test.run=^TestCompletionScriptHelper$ -- \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DEVTOOLS_COMPLETION_HELPER", "1")
	t.Setenv("DEVTOOLS_ISOLATED_ROOT", filepath.Join(helperDir, "store"))
	t.Setenv("PATH", helperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// User .zshenv can replace PATH and invoke the installed binary instead.
	t.Setenv("ZDOTDIR", helperDir)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			code, script, stderr := invoke(t, a, "", "completion", shell)
			if code != 0 || stderr != "" || !strings.Contains(script, "__complete --describe") || !strings.Contains(script, "dvt") {
				t.Fatalf("%d %s", code, stderr)
			}
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell unavailable")
			}
			path := filepath.Join(privateTempDir(t), "completion")
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
				{[]string{"devtools", "--f"}, "--format", false},
				{[]string{"devtools", "--format", ""}, "json", false},
				{[]string{"devtools", "--format=t"}, "--format=text", false},
				{[]string{"devtools", "--format", "json", "command", ""}, "list", false},
				{[]string{"devtools", "--isolated-root", "/tmp/fixture", "--format", "text", "command", ""}, "list", false},
				{[]string{"devtools", "var", ""}, "get", false},
				{[]string{"devtools", "variable", "set", "KEY", ""}, "--value", false},
				{[]string{"devtools", "variable", "set", "KEY", ""}, "--stdin", false},
				{[]string{"devtools", "var", "set", "KEY", ""}, "--file", false},
				{[]string{"devtools", "task", "workstream", ""}, "create", false},
				{[]string{"devtools", "proxy", ""}, "start", false},
				{[]string{"devtools", "run", ""}, "--no-proxy", false},
				{[]string{"devtools", "process", "start", "web", ""}, "--no-proxy", false},
				{[]string{"devtools", "project", "up", "web", ""}, "--no-proxy", false},
				{[]string{"devtools", "var", "set", "KEY", "--profile", ""}, "", true},
				{[]string{"devtools", "var", "set", "KEY", "--profile", "demo", ""}, "--value", false},
				{[]string{"devtools", "var", "set", "KEY", "--profile", "demo", "--pro"}, "", true},
				{[]string{"devtools", "var", "set", "KEY", "--stdin", "--val"}, "", true},
				{[]string{"devtools", "var", "set", "KEY", "--stdin=false", "--val"}, "--value", false},
				{[]string{"devtools", "run", "--proxy=false", "--no-pro"}, "", true},
				{[]string{"devtools", "doctor", "--storage", "--pro"}, "", true},
				{[]string{"devtools", "config", "fmt", "--check", "--dry"}, "", true},
				{[]string{"devtools", "run", "--", ""}, "", true},
				{[]string{"devtools", "run", "--env", "local", ""}, "--profile", false},
				{[]string{"devtools", "run", "web", ""}, "", true},
				{[]string{"devtools", "run", "web", "--env", ""}, "", true},
				{[]string{"devtools", "--format", "json", "run", "web", "--format", ""}, "", true},
				{[]string{"devtools", "command", "run", "web", ""}, "", true},
				{[]string{"devtools", "cmd", "run", "web", "--profile=", ""}, "", true},
				{[]string{"devtools", "completion", ""}, "fish", false},
				{[]string{"dvt", "var", ""}, "get", false},
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
				command := exec.Command(binary, "-c", harness)
				command.Dir = helperDir
				out, err := command.CombinedOutput()
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

func TestCompletionScriptHelper(t *testing.T) {
	if os.Getenv("DEVTOOLS_COMPLETION_HELPER") != "1" {
		return
	}
	for i, argument := range os.Args {
		if argument == "--" {
			os.Exit(New("test", "test").Run(context.Background(), os.Args[i+1:], IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}))
		}
	}
	t.Fatal("missing helper arguments")
}

func TestCompletionUsesInvokedExecutable(t *testing.T) {
	a := New("test", "test")
	shadow := privateTempDir(t)
	selected := filepath.Join(privateTempDir(t), "selected build")
	if err := os.Mkdir(selected, 0700); err != nil {
		t.Fatal(err)
	}
	for directory, candidate := range map[string]string{shadow: "PATH-candidate", selected: "selected-candidate"} {
		if err := os.WriteFile(filepath.Join(directory, "devtools"), []byte("#!/bin/sh\nprintf '%s\\n' "+shellQuote(candidate)+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ZDOTDIR", shadow)
	target := filepath.Join(selected, "devtools")
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell unavailable")
			}
			code, script, stderr := invoke(t, a, "", "completion", shell)
			if code != 0 || stderr != "" {
				t.Fatal(code, stderr)
			}
			path := filepath.Join(privateTempDir(t), "completion")
			if err := os.WriteFile(path, []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			var harness string
			switch shell {
			case "bash":
				harness = "source " + shellQuote(path) + "\nCOMP_WORDS=(" + shellQuote(target) + " command explain '')\nCOMP_CWORD=3\n_devtools\nprintf '%s\\n' \"${COMPREPLY[@]}\""
			case "zsh":
				harness = "compdef() { :; }\n_describe() { print -rl -- $candidates; }\nsource " + shellQuote(path) + "\nwords=(" + shellQuote(target) + " command explain '')\nCURRENT=4\n_devtools"
			case "fish":
				harness = "source " + shellQuote(path) + "\ncomplete -C " + shellQuote(shellQuote(target)+" command explain ")
			}
			out, err := exec.Command(binary, "-c", harness).CombinedOutput()
			if err != nil || !strings.Contains(string(out), "selected-candidate") || strings.Contains(string(out), "PATH-candidate") {
				t.Fatalf("completion queried a different executable: %s %v", out, err)
			}
		})
	}
}
