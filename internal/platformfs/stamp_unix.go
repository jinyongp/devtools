//go:build !windows

package platformfs

import (
	"fmt"
	"syscall"
)

// PrivateStamp identifies the file publication at path using checked metadata.
func PrivateStamp(path string) (string, error) {
	info, err := StatPrivate(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrUnsafe
	}
	return fmt.Sprintf("%v/%v/%v/%v", stat.Dev, stat.Ino, info.ModTime().UnixNano(), info.Size()), nil
}
