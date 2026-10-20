package paths

import (
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
	"path/filepath"
	"strings"
)

func currentPlatformDirectories() (Directories, error) {
	// Query the OS rather than trusting LOCALAPPDATA/XDG environment overrides.
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return Directories{}, err
	}
	return windowsDirectories(base), nil
}

func absoluteIsolatedPath(root string) (string, error) {
	if filepath.VolumeName(root) != "" && !filepath.IsAbs(root) {
		return "", platformfs.ErrUnsafe
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return platformfs.ValidatePrivatePath(absolute)
}

func canonicalIsolation(path string) (string, error) {
	boundary, err := platformfs.WindowsPrivatePathBoundary(path)
	if err != nil {
		return "", err
	}
	for ancestor := path; ; ancestor = filepath.Dir(ancestor) {
		ptr, err := windows.UTF16PtrFromString(`\\?\` + ancestor)
		if err != nil {
			return "", err
		}
		attributes, err := windows.GetFileAttributes(ptr)
		if err == nil && attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return "", platformfs.ErrUnsafe
		}
		if err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return "", err
		}
		if strings.EqualFold(ancestor, boundary) {
			break
		}
	}
	return path, nil
}
