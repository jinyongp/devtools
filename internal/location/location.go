// Package location canonicalizes filesystem paths used as persistent identities.
package location

import (
	"fmt"
	"os"
)

// Canonical returns an absolute path with filesystem aliases and symlinks
// resolved. Missing suffixes are preserved after resolving the deepest
// existing ancestor, so callers can compare identities for paths that no
// longer exist without rewriting stored records.
func Canonical(path string) (string, error) { return canonicalPath(path) }

// Same reports whether two path spellings identify the same canonical location.
// Exact equality is accepted even when the location is no longer resolvable.
func Same(left, right string) bool { return samePaths(left, right) }

// ExistingDirectory canonicalizes path and requires it to identify an existing
// directory.
func ExistingDirectory(path string) (string, error) {
	canonical, err := Canonical(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", canonical)
	}
	return canonical, nil
}
