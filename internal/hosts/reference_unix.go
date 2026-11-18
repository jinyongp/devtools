//go:build !windows

package hosts

import (
	"fmt"
	"path/filepath"
	"syscall"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func referenceStamp(root string) string {
	info, err := platformfs.StatPrivate(filepath.Join(root, "references.json"))
	if err != nil {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v/%v/%v/%v", stat.Dev, stat.Ino, info.ModTime().UnixNano(), info.Size())
}
