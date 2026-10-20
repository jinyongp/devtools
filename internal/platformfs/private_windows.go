package platformfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type directoryGuard struct{ handles []windows.Handle }

func (g *directoryGuard) close() error {
	var err error
	for i := len(g.handles) - 1; i >= 0; i-- {
		err = errors.Join(err, windows.CloseHandle(g.handles[i]))
	}
	return err
}

func privatePath(path string) (string, error) {
	if path == "" || strings.HasPrefix(path, `\\`) {
		return "", fsError("validate_path", path, "invalid_path", ErrUnsafe)
	}
	// Reject drive-relative, device, stream and Win32-normalized aliases before
	// adding an internal extended path prefix for long-path operations.
	if len(filepath.VolumeName(path)) != 2 || !filepath.IsAbs(path) {
		return "", fsError("validate_path", path, "invalid_path", ErrUnsafe)
	}
	for _, part := range strings.FieldsFunc(path[2:], func(r rune) bool { return r == '\\' || r == '/' }) {
		if reservedComponent(part) || strings.ContainsRune(part, ':') || strings.TrimRight(part, " .") != part {
			return "", fsError("validate_path", path, "invalid_path", ErrUnsafe)
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsLocal(strings.TrimPrefix(absolute, filepath.VolumeName(absolute)+`\`)) {
		return "", fsError("validate_path", path, "invalid_path", ErrUnsafe)
	}
	return absolute, nil
}
func reservedComponent(part string) bool {
	base, _, _ := strings.Cut(part, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if strings.HasPrefix(base, prefix) {
			digit := strings.TrimPrefix(base, prefix)
			if len(digit) == 1 && digit[0] >= '1' && digit[0] <= '9' {
				return true
			}
			if digit == "¹" || digit == "²" || digit == "³" {
				return true
			}
		}
	}
	return false
}

func pathPtr(path string) (*uint16, error) { return windows.UTF16PtrFromString(`\\?\` + path) }

func checkPrivate(h windows.Handle, directory bool) (err error) {
	defer func() { err = securityError(directory, "", err) }()
	identity, err := currentStorageIdentity()
	if err != nil {
		return err
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return identity.check(sd, directory)
}

func guardDirectory(path, anchor string, create bool) (guard *directoryGuard, err error) {
	defer func() { err = fsError("open_directory", path, "", err) }()
	guard = &directoryGuard{}
	defer func(g *directoryGuard) {
		if err != nil {
			_ = g.close()
		}
	}(guard)
	start, err := storageDirectoryStart(path)
	if err != nil {
		return nil, err
	}
	current := start
	relative, err := filepath.Rel(start, path)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(relative, `\`)
	if relative == "." {
		parts = nil
	}
	// Pin the complete chain from the trusted boundary without share-delete.
	// Usually that is the drive root. AppContainer-local paths instead start at
	// the OS-provided container Known Folder; its host ancestors are inaccessible
	// to the container. Never skip a failed intermediate open or reparse check.
	for i := -1; i < len(parts); i++ {
		if i >= 0 {
			if parts[i] == "" {
				continue
			}
			current = filepath.Join(current, parts[i])
		}
		ptr, e := pathPtr(current)
		if e != nil {
			return nil, e
		}
		if create && i >= 0 {
			_, sd, e := privateSecurity()
			if e != nil {
				return nil, e
			}
			sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
			if e = windows.CreateDirectory(ptr, &sa); e != nil && !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
				return nil, fsError("create_directory", current, "", e)
			}
		}
		h, e := windows.CreateFile(ptr, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if e != nil {
			return nil, fsError("open_directory", current, "", e)
		}
		guard.handles = append(guard.handles, h)
		var info windows.ByHandleFileInformation
		if e = windows.GetFileInformationByHandle(h, &info); e != nil {
			return nil, fsError("inspect_directory", current, "", e)
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return nil, fsError("inspect_directory", current, "reparse_or_non_directory", ErrUnsafe)
		}
		if i == -1 {
			// Validate normalized components on this already opened NTFS volume.
			// Asking Windows to translate its device into a DOS drive name can
			// require access outside an AppContainer's trusted profile boundary.
			actual, e := normalizedHandlePath(h, volumeNameNone)
			if e != nil {
				return nil, fsError("inspect_directory", current, "", e)
			}
			if !strings.EqualFold(actual, strings.TrimPrefix(start, filepath.VolumeName(start))) {
				return nil, fsError("inspect_directory", current, "path_identity_mismatch", ErrUnsafe)
			}
			fs := make([]uint16, 32)
			if e = windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); e != nil {
				return nil, fsError("inspect_filesystem", current, "", e)
			}
			drive, e := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
			if e != nil {
				return nil, e
			}
			if windows.UTF16ToString(fs) != "NTFS" || windows.GetDriveType(drive) == windows.DRIVE_REMOTE {
				return nil, fsError("inspect_filesystem", current, "unsupported_filesystem", ErrUnsafe)
			}
		}
		if storagePathWithin(anchor, current) {
			if e = checkPrivate(h, true); e != nil {
				return nil, e
			}
		}
	}
	return guard, nil
}

func openPrivateFile(path string, flags int) (*os.File, error) {
	// A concurrent MoveFileEx holds DELETE access briefly. A reader that excludes
	// share-delete must wait for that publisher, then validate the opened snapshot.
	// Keep the file/parent pins and never retry an ACL or invalid-image failure.
	if flags != os.O_RDONLY {
		return openPrivateFileWithShare(path, flags, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	}
	deadline := time.Now().Add(2 * time.Second)
	delay := 10 * time.Millisecond
	for {
		file, err := openPrivateFileWithShare(path, flags, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return file, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, err
		}
		if delay > remaining {
			delay = remaining
		}
		time.Sleep(delay)
		delay *= 2
		if delay > 200*time.Millisecond {
			delay = 200 * time.Millisecond
		}
	}
}

func openPrivateFileWithShare(path string, flags int, share uint32) (*os.File, error) {
	access := uint32(windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES)
	if flags&os.O_WRONLY == 0 {
		access |= windows.GENERIC_READ
	}
	if flags&(os.O_WRONLY|os.O_RDWR) != 0 {
		if flags&os.O_APPEND != 0 {
			access |= windows.FILE_APPEND_DATA
		} else {
			access |= windows.GENERIC_WRITE
		}
	}
	if flags&os.O_APPEND != 0 && flags&os.O_TRUNC != 0 {
		return nil, fmt.Errorf("append and truncate cannot be combined")
	}
	ptr, err := pathPtr(path)
	if err != nil {
		return nil, err
	}
	disposition := uint32(windows.OPEN_EXISTING)
	if flags&os.O_CREATE != 0 {
		if flags&os.O_EXCL != 0 {
			disposition = windows.CREATE_NEW
		} else {
			disposition = windows.OPEN_ALWAYS
		}
	}
	_, sd, err := privateSecurity()
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := windows.CreateFile(ptr, access, share, &sa, disposition, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, fsError("open_file", path, "", &os.PathError{Op: "open", Path: path, Err: err})
	}
	fail := func(e error) (*os.File, error) { _ = windows.CloseHandle(h); return nil, e }
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		return fail(err)
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return fail(fsError("inspect_file", path, "reparse_or_non_regular_file", ErrUnsafe))
	}
	if info.NumberOfLinks != 1 {
		return fail(fsError("inspect_file", path, "hard_link", ErrUnsafe))
	}
	if err = checkPrivate(h, false); err != nil {
		return fail(err)
	}
	f := os.NewFile(uintptr(h), path)
	return f, nil
}

func publish(ctx context.Context, from, to string, replace bool) (err error) {
	defer func() { err = fsError("publish_file", to, "", err) }()
	fromPtr, err := pathPtr(from)
	if err != nil {
		return err
	}
	toPtr, err := pathPtr(to)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	delay := 10 * time.Millisecond
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		// Recheck file ACL/identity on every retry. Parent guards are held by the
		// caller; file handles must close before MoveFileEx can replace the image.
		err = checkPublicationImages(from, to)
		if err == nil {
			flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
			if replace {
				flags |= windows.MOVEFILE_REPLACE_EXISTING
			}
			err = windows.MoveFileEx(fromPtr, toPtr, flags)
		}
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		if delay > remaining {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay *= 2
		if delay > 200*time.Millisecond {
			delay = 200 * time.Millisecond
		}
	}
}

const (
	volumeNameNT   = 0x2
	volumeNameNone = 0x4
)

func handlePath(h windows.Handle) (string, error) {
	// Keep normalized physical volume identity for containment checks without
	// requiring a DOS drive-letter mapping. Both compared names use this form.
	return normalizedHandlePath(h, volumeNameNT)
}

func normalizedHandlePath(h windows.Handle, volume uint32) (string, error) {
	buffer := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), volume)
		if err != nil {
			return "", err
		}
		if n < uint32(len(buffer)) {
			return windows.UTF16ToString(buffer[:n]), nil
		}
		buffer = make([]uint16, n+1)
	}
}
func containedFile(r *Root, f *os.File) error {
	base, err := handlePath(r.guard.handles[len(r.guard.handles)-1])
	if err != nil {
		return err
	}
	actual, err := handlePath(windows.Handle(f.Fd()))
	if err != nil {
		return err
	}
	// Both names come from normalized handles, preserving NTFS case-sensitive
	// directories rather than lowercasing user-supplied paths.
	if !strings.HasPrefix(actual, strings.TrimRight(base, `\`)+`\`) {
		return fsError("verify_file_identity", f.Name(), "path_mismatch", ErrUnsafe)
	}
	return nil
}

func checkPublicationImages(from, to string) error {
	source, err := openPrivateFileWithShare(from, os.O_RDONLY, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	if err != nil {
		return err
	}
	if err = source.Close(); err != nil {
		return err
	}
	target, err := openPrivateFileWithShare(to, os.O_RDONLY, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return target.Close()
}
