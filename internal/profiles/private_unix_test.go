//go:build !windows

package profiles

import "os"

func makeProfileDirectoryPublic(path string) error { return os.Chmod(path, 0755) }
