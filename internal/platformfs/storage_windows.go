package platformfs

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unicode/utf16"
	"unsafe"
)

// OpenPrivate retains checked ancestors until Close. Windows storage callers
// never substitute POSIX mode bits or a zero-valued no-follow flag for ACL checks.
func OpenPrivate(path string, flags int, _ os.FileMode) (*File, error) {
	root, err := OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(filepath.Base(path), flags)
}
func CheckFile(f *os.File) error {
	var info windows.ByHandleFileInformation
	h := windows.Handle(f.Fd())
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1 {
		return securityError(false, "reparse_or_non_regular_file", ErrUnsafe)
	}
	return checkPrivate(h, false)
}
func CheckDirectory(path string) error {
	root, err := OpenRoot(path, false)
	if err != nil {
		return err
	}
	return root.Close()
}

func DirectoryPrivate(path string, info os.FileInfo) bool {
	return info != nil && info.IsDir() && CheckDirectory(path) == nil
}
func MkdirPrivate(path string) error {
	root, err := OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer root.Close()
	checked, err := root.child(filepath.Base(path))
	if err != nil {
		return err
	}
	ptr, err := pathPtr(checked)
	if err != nil {
		return err
	}
	_, sd, err := privateSecurity()
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	return windows.CreateDirectory(ptr, &sa)
}
func StatPrivate(path string) (os.FileInfo, error) {
	f, err := OpenPrivate(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

func NameTooLong(path string, err error) bool {
	if err == nil {
		return false
	}
	// NTFS private storage has a 255 UTF-16-code-unit component limit. Windows
	// reports overlong legacy hex components as either range or invalid-name.
	return errors.Is(err, windows.ERROR_FILENAME_EXCED_RANGE) || len(utf16.Encode([]rune(filepath.Base(path)))) > 255 && (errors.Is(err, windows.ERROR_INVALID_NAME) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND))
}
