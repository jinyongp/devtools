package platformfs

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// OpenExclusive opens a private file with share mode zero. Unlike byte-range
// locks, its exclusion survives an explicitly inherited handle's parent close.
// Only the file handle is inheritable; checked parent handles remain local.
func (r *Root) OpenExclusive(name string, create, inheritable bool) (*File, error) {
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	f, err := r.openFile(name, flags, func(path string, flags int) (*os.File, error) { return openPrivateFileWithShare(path, flags, 0) })
	if err != nil {
		return nil, err
	}
	value := uint32(0)
	if inheritable {
		value = windows.HANDLE_FLAG_INHERIT
	}
	if err = windows.SetHandleInformation(windows.Handle(f.Fd()), windows.HANDLE_FLAG_INHERIT, value); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// TryLock acquires the single byte used by ordinary storage RW locks. Only
// ERROR_LOCK_VIOLATION is contention; permissions and invalid handles are errors.
func (f *File) TryLock(shared bool) (bool, error) {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if !shared {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}
func (f *File) Unlock() error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}

// ValidateFileHandle verifies identity and ACL through metadata-only access
// without taking ownership or weakening share-mode exclusion.
func (r *Root) ValidateFileHandle(name string, f *os.File) error {
	guard, err := r.checkedInheritedFile(name, f)
	if err != nil {
		return err
	}
	return guard.close()
}

// AdoptFile transfers ownership only on success and clears handle inheritance.
func (r *Root) AdoptFile(name string, f *os.File) (*File, error) {
	guard, err := r.checkedInheritedFile(name, f)
	if err != nil {
		return nil, err
	}
	if err = windows.SetHandleInformation(windows.Handle(f.Fd()), windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		_ = guard.close()
		return nil, err
	}
	return &File{File: f, guard: guard}, nil
}

func (r *Root) checkedInheritedFile(name string, f *os.File) (*directoryGuard, error) {
	if f == nil {
		return nil, ErrUnsafe
	}
	path, err := r.child(name)
	if err != nil {
		return nil, err
	}
	guard, err := guardDirectory(filepath.Dir(path), r.path, false)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*directoryGuard, error) { _ = guard.close(); return nil, e }
	ptr, err := pathPtr(path)
	if err != nil {
		return fail(err)
	}
	h, err := windows.CreateFile(ptr, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fail(err)
	}
	defer windows.CloseHandle(h)
	var expected, actual windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &expected); err != nil {
		return fail(err)
	}
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &actual); err != nil {
		return fail(err)
	}
	if expected.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || expected.NumberOfLinks != 1 || actual.NumberOfLinks != 1 || expected.VolumeSerialNumber != actual.VolumeSerialNumber || expected.FileIndexHigh != actual.FileIndexHigh || expected.FileIndexLow != actual.FileIndexLow {
		return fail(ErrUnsafe)
	}
	if err = checkPrivate(h, false); err != nil {
		return fail(err)
	}
	if err = containedFile(r, f); err != nil {
		return fail(err)
	}
	return guard, nil
}
