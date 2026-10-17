//go:build !windows

package location

import "os"

func createDirectoryAlias(actual, alias string) error { return os.Symlink(actual, alias) }
