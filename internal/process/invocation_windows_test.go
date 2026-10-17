package process

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWindowsArgvChild(t *testing.T) {
	mode := os.Getenv("DEVTOOLS_ARGV_CHILD")
	if mode == "" {
		return
	}
	switch mode {
	case "args":
		for i, arg := range os.Args {
			if arg == "--" {
				_ = json.NewEncoder(os.Stdout).Encode(os.Args[i+1:])
				os.Exit(0)
			}
		}
		os.Exit(90)
	case "streams":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		_, _ = io.WriteString(os.Stderr, "diagnostic")
		os.Exit(27)
	case "sleep":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	os.Exit(91)
}
func windowsChild(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "한글 with spaces")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "child.exe")
	if err = os.WriteFile(child, body, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, child
}
func TestWindowsCaseInsensitiveEnvironment(t *testing.T) {
	env := Environment([]string{`=C:=C:\work`, "Path=old", "VALUE=old"}, map[string]string{"PATH": "new", "VALUE": "new"})
	if selectedEnvironment(env, "path") != "new" || selectedEnvironment(env, "value") != "new" {
		t.Fatal("injected environment did not win")
	}
	if !strings.Contains(strings.Join(env, "\n"), `=C:=C:\work`) {
		t.Fatal("drive environment entry lost")
	}
	count := 0
	for _, entry := range env {
		key, _, _ := splitEnvironment(entry)
		if strings.EqualFold(key, "PATH") {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate case variants remain")
	}
	e := ValidateEnvironment(map[string]string{"Path": "secret-one", "PATH": "secret-two"})
	if e == nil || e.Code != "invalid_argument" {
		t.Fatal("ambiguous injection accepted")
	}
	encoded, _ := json.Marshal(e)
	if strings.Contains(string(encoded), "secret-") {
		t.Fatal("environment values exposed in error")
	}
	if keys, ok := e.Details["keys"].([]string); !ok || len(keys) != 2 {
		t.Fatal("conflicting key names missing")
	}
}
func TestWindowsSelectedPathAndPathext(t *testing.T) {
	dir, child := windowsChild(t)
	env := Environment(os.Environ(), map[string]string{"Path": dir, "PATHEXT": ".cmd;.exe"})
	got, e := LookPath("child", t.TempDir(), env)
	if e != nil || !strings.EqualFold(got, child) {
		t.Fatalf("PATH/PATHEXT lookup: %q %v", got, e)
	}
	got, e = LookPath(`.\child`, dir, env)
	if e != nil || !strings.EqualFold(got, child) {
		t.Fatalf("explicit relative extension lookup: %q %v", got, e)
	}
	if _, e = LookPath("child", dir, []string{"PATH=;", "PATHEXT=.EXE"}); e == nil || e.ExitCode != 127 {
		t.Fatal("implicit cwd search enabled")
	}
	if _, e = LookPath(`C:child`, dir, env); e == nil || e.Code != "invalid_argument" {
		t.Fatal("drive-relative executable accepted")
	}
}
func TestWindowsExeLiteralArgv(t *testing.T) {
	dir, child := windowsChild(t)
	args := []string{"", "two words", "한글", `a"b`, `C:\trailing\`, "%PATH%", "!bang!", "a&b", "a|b", "a^b", "(x)", "line\nfeed"}
	command := append([]string{child, "-test.run=TestWindowsArgvChild", "--"}, args...)
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_ARGV_CHILD": "args"})
	var out, diagnostic bytes.Buffer
	code, e := Execute(context.Background(), command, dir, env, nil, &out, &diagnostic)
	if e != nil || code != 0 {
		t.Fatalf("execute: %d %v %s", code, e, &diagnostic)
	}
	var got []string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("literal argv mismatch: %q", got)
	}
}
func TestWindowsBatchLiteralDomain(t *testing.T) {
	dir, _ := windowsChild(t)
	script := filepath.Join(dir, "fixture.cmd")
	if err := os.WriteFile(script, []byte("@\"%~dp0child.exe\" -test.run=TestWindowsArgvChild -- %*\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"", "two words", "한글", `C:\trailing\`, "!bang!", "a&b", "a|b", "a^b", "(x)", "<xml>"}
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_ARGV_CHILD": "args"})
	var out, diagnostic bytes.Buffer
	code, e := Execute(context.Background(), append([]string{script}, args...), dir, env, nil, &out, &diagnostic)
	if e != nil || code != 0 {
		t.Fatalf("batch: %d %v %s", code, e, &diagnostic)
	}
	var got []string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("batch argv mismatch: %q", got)
	}
	for _, arg := range []string{"%PATH%", `a"b`, "line\nfeed", "carriage\rreturn", "nul\x00byte"} {
		if _, e := prepareInvocation(context.Background(), []string{script, arg}, dir, env); e == nil || e.Code != "invalid_argument" {
			t.Fatalf("forbidden batch argument accepted: %v", e)
		}
	}
	badScript := filepath.Join(dir, "percent%script.cmd")
	if err := os.WriteFile(badScript, []byte("@exit /b 0\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, e := prepareInvocation(context.Background(), []string{badScript}, dir, env); e == nil || e.Code != "invalid_argument" {
		t.Fatal("percent script path accepted")
	}
}
func TestWindowsExplicitPowerShellAndStreams(t *testing.T) {
	dir, child := windowsChild(t)
	script := filepath.Join(dir, "fixture.ps1")
	if err := os.WriteFile(script, []byte("exit 0"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, e := prepareInvocation(context.Background(), []string{script}, dir, os.Environ()); e == nil || e.Code != "invalid_argument" {
		t.Fatal("PowerShell script executed implicitly")
	}
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_ARGV_CHILD": "streams"})
	var out, diagnostic bytes.Buffer
	code, e := Execute(context.Background(), []string{child, "-test.run=TestWindowsArgvChild"}, dir, env, strings.NewReader("input"), &out, &diagnostic)
	if e != nil || code != 27 || out.String() != "input" || diagnostic.String() != "diagnostic" {
		t.Fatalf("streams/exit: %d %v %q %q", code, e, &out, &diagnostic)
	}
}
func TestWindowsCancelledDirectChild(t *testing.T) {
	dir, child := windowsChild(t)
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_ARGV_CHILD": "sleep"})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	code, e := Execute(ctx, []string{child, "-test.run=TestWindowsArgvChild"}, dir, env, nil, io.Discard, io.Discard)
	if e != nil || code != 130 {
		t.Fatalf("cancel: %d %v", code, e)
	}
}
func TestWindowsRealNpmPnpmShims(t *testing.T) {
	if os.Getenv("DEVTOOLS_TEST_PACKAGE_SHIMS") != "1" {
		t.Skip("real Node/package-manager shims are supplied by native CI")
	}
	for _, name := range []string{"npm.cmd", "pnpm.cmd"} {
		var out, diagnostic bytes.Buffer
		code, e := Execute(context.Background(), []string{name, "--version"}, t.TempDir(), os.Environ(), nil, &out, &diagnostic)
		if e != nil || code != 0 || strings.TrimSpace(out.String()) == "" {
			t.Fatalf("real %s: %d %v %s", name, code, e, &diagnostic)
		}
	}
}

func TestWindowsActualPowerShellCallers(t *testing.T) {
	dir, child := windowsChild(t)
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	shells := []string{filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")}
	if modern, err := exec.LookPath("pwsh.exe"); err == nil {
		shells = append(shells, modern)
	} else if os.Getenv("CI") == "true" {
		t.Fatal("PowerShell 7 is required by native CI")
	}
	// PS5.1's legacy native parser drops empty arguments before our CLI sees
	// them. Empty argv is verified separately at our direct-executable boundary.
	script := filepath.Join(dir, "caller.ps1")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	body := "\ufeff$ErrorActionPreference = 'Stop'\r\n$OutputEncoding = [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)\r\n$env:DEVTOOLS_ARGV_CHILD = 'args'\r\n& " + quote(child) + " '-test.run=TestWindowsArgvChild' '--' 'two words' '한글' 'a&b'\r\nexit $LASTEXITCODE\r\n"
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for _, shell := range shells {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			command := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
			command.Dir = dir
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("shell caller: %v %s", err, output)
			}
			var got []string
			if err = json.Unmarshal(bytes.TrimPrefix(output, []byte{0xef, 0xbb, 0xbf}), &got); err != nil {
				t.Fatalf("shell output: %v %q", err, output)
			}
			if !reflect.DeepEqual(got, []string{"two words", "한글", "a&b"}) {
				t.Fatalf("shell argv: %q", got)
			}
		})
	}
}
