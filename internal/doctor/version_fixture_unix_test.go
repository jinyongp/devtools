//go:build !windows

package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func doctorVersionFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'tool 1.2.3 fixture-secret'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
