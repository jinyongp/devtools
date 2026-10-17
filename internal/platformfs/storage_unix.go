//go:build !windows

package platformfs

import (
	"errors"
	"os"
	"syscall"
)

// OpenPrivate preserves the Unix no-follow/nonblocking open contract. Store
// parsers retain their existing regular-file and permission checks.
func OpenPrivate(path string, flags int, perm os.FileMode) (*File, error) {
	f, err := OpenNoFollow(path, flags, perm)
	if err != nil {
		return nil, err
	}
	return &File{File: f, guard: &directoryGuard{}}, nil
}

func CheckFile(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return ErrUnsafe
	}
	return nil
}
func CheckDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrUnsafe
	}
	return nil
}

func DirectoryPrivate(_ string, info os.FileInfo) bool {
	return info != nil && info.IsDir() && info.Mode().Perm()&0077 == 0
}
func MkdirPrivate(path string) error { return os.Mkdir(path, 0700) }
func StatPrivate(path string) (os.FileInfo, error) {
	f, err := OpenPrivate(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err = CheckFile(f.File); err != nil {
		return nil, err
	}
	return f.Stat()
}

// NameTooLong classifies an unaddressable legacy component without weakening
// ordinary missing/permission/corruption errors.
func NameTooLong(_ string, err error) bool { return errors.Is(err, syscall.ENAMETOOLONG) }
