//go:build !windows

package hosts

import (
	"os"
	"testing"
)

func prepareHostsTestFile(t *testing.T, path string) {}
func assertHostsTestPermissions(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal("hosts fixture mode changed", info, err)
	}
}
