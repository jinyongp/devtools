package storageerr

import (
	"errors"
	"golang.org/x/sys/windows"
)

func osReason(err error) string {
	switch {
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return "sharing_violation"
	case errors.Is(err, windows.ERROR_DISK_FULL), errors.Is(err, windows.ERROR_HANDLE_DISK_FULL):
		return "disk_full"
	case errors.Is(err, windows.ERROR_WRITE_PROTECT):
		return "read_only"
	case errors.Is(err, windows.ERROR_FILENAME_EXCED_RANGE):
		return "path_too_long"
	}
	return "unknown"
}
