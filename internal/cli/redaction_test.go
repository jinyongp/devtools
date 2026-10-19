package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForegroundRealBinaryExceptionAndStderrMasking(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	canary := "synthetic-byte-token"
	if code, _, err := invoke(t, app, canary, "sec", "set", "TOKEN", "--stdin", "--profile", "redaction-fixture"); code != 0 {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	run := func(options ...string) (int, string, string) {
		args := append([]string{"run", "--profile", "redaction-fixture"}, options...)
		args = append(args, "--", exe, "__redaction-test-once")
		return invoke(t, app, "", args...)
	}
	code, out, diagnostic := run()
	if code != 23 || !bytes.Equal([]byte(out), append(append([]byte{0, 0xff}, []byte("[REDACTED]")...), 0x80)) || diagnostic != "[REDACTED]" {
		t.Fatal("default streams or exit code were not preserved safely")
	}
	code, out, diagnostic = run("--stdout", "raw")
	if code != 23 || !bytes.Contains([]byte(out), []byte(canary)) || diagnostic != "[REDACTED]" {
		t.Fatal("stdout exception affected stderr or exit code")
	}
	code, out, diagnostic = run("--output", "raw")
	if code != 23 || !bytes.Contains([]byte(out), []byte(canary)) || diagnostic != canary {
		t.Fatal("explicit all-raw mode not honored")
	}
}

func TestMaskerLimitRejectsBeforeForegroundChildStarts(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	// This exceeds the matcher budget while remaining a valid Windows UTF-16
	// variable and a valid Unix environment string, so the output gate is tested.
	large := strings.Repeat("가", 22000)
	if code, _, err := invoke(t, app, large, "sec", "set", "TOKEN", "--stdin", "--profile", "redaction-limit"); code != 0 {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	marker := filepath.Join(privateTempDir(t), "must-not-exist")
	code, out, diagnostic := invoke(t, app, "", "run", "--profile", "redaction-limit", "--", exe, "__launcher-test-once", marker)
	if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"redaction_unavailable"`) {
		t.Fatal("limit did not reject through the public CLI")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("child started before masker initialization", err)
	}
	if strings.Contains(diagnostic, large) {
		t.Fatal("failure disclosed pattern")
	}
}
