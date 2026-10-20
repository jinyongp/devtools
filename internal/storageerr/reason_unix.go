//go:build !windows

package storageerr

import (
	"errors"
	"syscall"
)

func osReason(err error) string {
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return "disk_full"
	case errors.Is(err, syscall.EROFS):
		return "read_only"
	case errors.Is(err, syscall.ENAMETOOLONG):
		return "path_too_long"
	}
	return "unknown"
}
