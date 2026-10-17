//go:build !windows

package platformfs

import "os"

func OpenExternalPrivate(path string, flags int, mode os.FileMode) (*File, error) {
	return OpenPrivate(path, flags, mode)
}
func EnsureExternalDirectory(path string) error { return os.MkdirAll(path, 0700) }
