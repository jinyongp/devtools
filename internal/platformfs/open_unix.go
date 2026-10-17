//go:build !windows

package platformfs

import (
	"os"
	"syscall"
)

// OpenNoFollow preserves the existing Unix open contract while callers migrate
// their private storage to Root. It is unavailable on Windows until those
// callers use the ACL/parent-handle boundary; zero-valued Unix flags are unsafe.
func OpenNoFollow(path string, flags int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, perm)
}
