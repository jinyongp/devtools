//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func commandFixture(t *testing.T, kind string) []string {
	t.Helper()
	script := ""
	switch kind {
	case "true":
		path, err := exec.LookPath("true")
		if err != nil {
			t.Fatal(err)
		}
		return []string{path}
	case "version", "version-bad":
		version := "1.2.3"
		if kind == "version-bad" {
			version = "1.2.30"
		}
		return []string{"/bin/sh", "-c", "echo fixture " + version + " CANARY-SECRET"}
	case "dev", "zeta":
		return []string{"/bin/sh", "-c", "printf " + kind}
	case "check-marker":
		script = `test -n "$TOKEN" && printf ran > marker`
	case "bind":
		return []string{"/bin/sh", "-c", `printf '%s|%s' "$URL" "$1"`, "--"}
	case "output-fail":
		script = `printf output; printf diagnostic >&2; exit 17`
	case "cwd-level-first":
		return []string{"/bin/sh", "-c", `printf '%s|%s|%s' "$PWD" "$LEVEL" "$1"`, "label"}
	case "cwd-level":
		script = `printf '%s|%s' "$PWD" "$LEVEL"`
	case "level":
		script = `printf '%s' "$LEVEL"`
	case "marker":
		script = `printf '%s' "$MARKER"`
	case "token":
		script = `printf '%s' "$TOKEN"`
	case "raw-fail":
		script = `printf raw; printf diagnostic >&2; exit 23`
	case "secret-and-true":
		script = `printf '%s' "$TOKEN"; printf true`
	case "plain":
		script = `printf plain`
	case "cat":
		script = `cat`
	case "hold":
		script = `trap 'exit 0' TERM INT; while :; do sleep 1; done`
	default:
		t.Fatal("unknown command fixture", kind)
	}
	return []string{"/bin/sh", "-c", script}
}

func versionToolFixture(t *testing.T, bad bool) string {
	t.Helper()
	version := "1.2.3"
	if bad {
		version = "1.2.30"
	}
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho fixture "+version+" CANARY-SECRET\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
