//go:build !windows

package testfixture

import "os"

func MakePublic(path string) error { return os.Chmod(path, 0755) }
