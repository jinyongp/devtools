//go:build !windows

package cli

import "testing"

func runBoundaryCommand(t *testing.T) []string {
	t.Helper()
	return []string{"/bin/sh", "-c", `printf '%s|' "$LEVEL"; printf '<%s>' "$@"`, "label"}
}
