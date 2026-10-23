// Package testfs selects physical paths for owned test fixtures. Product path
// validation must retain its no-follow/containment checks instead of using this.
package testfs

import (
	"path/filepath"
	"testing"
)

// CanonicalDir resolves an existing fixture root, including macOS temporary
// aliases and Windows short names. Call it before deriving not-yet-created paths.
func CanonicalDir(t testing.TB, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		t.Fatal(err)
	}
	return physical
}

// TempDir keeps testing's ownership and cleanup while selecting the physical root.
// Callers remain responsible for their existing private permissions and ACLs.
func TempDir(t testing.TB) string {
	t.Helper()
	return CanonicalDir(t, t.TempDir())
}
