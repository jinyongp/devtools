//go:build !windows

package location

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func canonicalAbsolute(path string) (string, error) { return filepath.Abs(path) }

func samePaths(left, right string) bool {
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

func canonicalPath(path string) (string, error) {
	absolute, err := canonicalAbsolute(path)
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
