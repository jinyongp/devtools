//go:build !windows

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func currentPlatformDirectories() (Directories, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Directories{}, err
	}
	return Resolve(runtime.GOOS, home, os.Getenv)
}

func absoluteIsolatedPath(root string) (string, error) { return filepath.Abs(root) }

func canonicalIsolation(abs string) (string, error) {
	// Resolve existing ancestors so aliases cannot accidentally select two daemons.
	ancestor, suffix := abs, []string{}
	for {
		if resolved, e := filepath.EvalSymlinks(ancestor); e == nil {
			abs = resolved
			for i := len(suffix) - 1; i >= 0; i-- {
				abs = filepath.Join(abs, suffix[i])
			}
			break
		} else if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("cannot resolve isolation root")
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	return abs, nil
}
