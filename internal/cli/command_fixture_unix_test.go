//go:build !windows

package cli

import "testing"

func commandFixture(t *testing.T, kind string) []string {
	t.Helper()
	script := ""
	switch kind {
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
