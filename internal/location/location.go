// Package location canonicalizes filesystem paths used as persistent identities.
package location

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Canonical returns an absolute path with filesystem aliases and symlinks
// resolved. Missing suffixes are preserved after resolving the deepest
// existing ancestor, so callers can compare identities for paths that no
// longer exist without rewriting stored records.
func Canonical(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	current := absolute
	missing := []string{}
	for {
		_, err := os.Lstat(current)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			if len(missing) > 0 {
				info, err := os.Stat(resolved)
				if err != nil {
					return "", err
				}
				if !info.IsDir() {
					return "", fmt.Errorf("existing path prefix is not a directory: %s", resolved)
				}
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// Same reports whether two path spellings identify the same canonical location.
// Exact equality is accepted even when the location is no longer resolvable.
func Same(left, right string) bool {
	if left == right {
		return true
	}
	if left == "" || right == "" {
		return false
	}
	leftCanonical, leftErr := Canonical(left)
	if leftErr != nil {
		return false
	}
	rightCanonical, rightErr := Canonical(right)
	return rightErr == nil && leftCanonical == rightCanonical
}

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
