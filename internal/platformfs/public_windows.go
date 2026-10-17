package platformfs

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

// OpenPublicNoFollow pins a local NTFS path without claiming its ACL is private.
// It never creates, repairs or replaces the file or its security descriptor.
// Holding the wrapper excludes data writers and pins all parents. Writable mode
// permits deletion sharing so the caller can atomically replace the held file.
func OpenPublicNoFollow(path string, writable bool) (*File, error) {
	absolute, err := externalPath(path)
	if err != nil {
		return nil, err
	}
	guard, err := guardDirectory(filepath.Dir(absolute), "", false)
	if err != nil {
		return nil, err
	}
	ptr, err := pathPtr(absolute)
	if err != nil {
		guard.close()
		return nil, err
	}
	access := uint32(windows.GENERIC_READ | windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES)
	if writable {
		access |= windows.GENERIC_WRITE | windows.ACCESS_SYSTEM_SECURITY
	}
	share := uint32(windows.FILE_SHARE_READ)
	if writable {
		share |= windows.FILE_SHARE_DELETE
	}
	handle, err := windows.CreateFile(ptr, access, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		guard.close()
		return nil, err
	}
	file := os.NewFile(uintptr(handle), absolute)
	fail := func(e error) (*File, error) { file.Close(); guard.close(); return nil, e }
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fail(err)
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1 {
		return fail(ErrUnsafe)
	}
	if err = containedFile(&Root{path: filepath.Dir(absolute), guard: guard}, file); err != nil {
		return fail(err)
	}
	return &File{File: file, guard: guard}, nil
}
