//go:build !windows

package testfs

import "path/filepath"

func canonicalDir(path string) (string, error) { return filepath.EvalSymlinks(path) }
