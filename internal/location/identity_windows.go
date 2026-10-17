package location

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func canonicalAbsolute(path string) (string, error) {
	if filepath.VolumeName(path) != "" && !filepath.IsAbs(path) {
		return "", platformfs.ErrUnsafe
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return platformfs.ValidatePrivatePath(absolute)
}

type ancestorIdentity struct {
	info          os.FileInfo
	missing       []string
	caseSensitive bool
}

func identity(path string) (ancestorIdentity, error) {
	absolute, err := canonicalAbsolute(path)
	if err != nil {
		return ancestorIdentity{}, err
	}
	var missing []string
	for {
		info, err := os.Stat(absolute)
		if err == nil {
			if len(missing) == 0 {
				return ancestorIdentity{info: info}, nil
			}
			if !info.IsDir() {
				return ancestorIdentity{}, os.ErrInvalid
			}
			ptr, err := windows.UTF16PtrFromString(absolute)
			if err != nil {
				return ancestorIdentity{}, err
			}
			h, err := windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
			if err != nil {
				return ancestorIdentity{}, err
			}
			defer windows.CloseHandle(h)
			var flags uint32
			if err = windows.GetFileInformationByHandleEx(h, windows.FileCaseSensitiveInfo, (*byte)(unsafe.Pointer(&flags)), 4); err != nil {
				return ancestorIdentity{}, err
			}
			return ancestorIdentity{info: info, missing: missing, caseSensitive: flags&windows.FILE_CS_FLAG_CASE_SENSITIVE_DIR != 0}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return ancestorIdentity{}, err
		}
		parent := filepath.Dir(absolute)
		if parent == absolute {
			return ancestorIdentity{}, err
		}
		missing = append(missing, filepath.Base(absolute))
		absolute = parent
	}
}
func samePaths(left, right string) bool {
	if left == right {
		return true
	}
	if left == "" || right == "" {
		return false
	}
	a, err := identity(left)
	if err != nil {
		return false
	}
	b, err := identity(right)
	if err != nil || !os.SameFile(a.info, b.info) || len(a.missing) != len(b.missing) {
		return false
	}
	for i := range a.missing {
		if a.caseSensitive || b.caseSensitive {
			if a.missing[i] != b.missing[i] {
				return false
			}
		} else if !strings.EqualFold(a.missing[i], b.missing[i]) {
			return false
		}
	}
	return true
}

func canonicalPath(path string) (string, error) {
	absolute, err := canonicalAbsolute(path)
	if err != nil {
		return "", err
	}
	current := absolute
	var missing []string
	for {
		info, err := os.Stat(current)
		if err == nil {
			if len(missing) > 0 && !info.IsDir() {
				return "", os.ErrInvalid
			}
			ptr, err := windows.UTF16PtrFromString(`\\?\` + current)
			if err != nil {
				return "", err
			}
			h, err := windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
			if err != nil {
				return "", err
			}
			defer windows.CloseHandle(h)
			fs := make([]uint16, 32)
			if err = windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); err != nil {
				return "", err
			}
			if windows.UTF16ToString(fs) != "NTFS" {
				return "", platformfs.ErrUnsafe
			}
			buffer := make([]uint16, 512)
			for {
				n, err := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), 0)
				if err != nil {
					return "", err
				}
				if n >= uint32(len(buffer)) {
					buffer = make([]uint16, n+1)
					continue
				}
				resolved := strings.TrimPrefix(windows.UTF16ToString(buffer[:n]), `\\?\`)
				if strings.HasPrefix(resolved, `UNC\`) {
					return "", platformfs.ErrUnsafe
				}
				for i := len(missing) - 1; i >= 0; i-- {
					resolved = filepath.Join(resolved, missing[i])
				}
				return filepath.Clean(resolved), nil
			}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
