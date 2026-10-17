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
		return "", ErrUnsafe
	}
	// Reject drive-relative, device, stream and Win32-normalized aliases before
	// adding an internal extended path prefix for long-path operations.
	if len(filepath.VolumeName(path)) != 2 || !filepath.IsAbs(path) {
		return "", ErrUnsafe
	}
	for _, part := range strings.FieldsFunc(path[2:], func(r rune) bool { return r == '\\' || r == '/' }) {
		if reservedComponent(part) || strings.ContainsRune(part, ':') || strings.TrimRight(part, " .") != part {
			return "", ErrUnsafe
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsLocal(strings.TrimPrefix(absolute, filepath.VolumeName(absolute)+`\`)) {
		return "", ErrUnsafe
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

func userSecurity() (*windows.SID, *windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	sid := user.User.Sid
	sd, err := windows.SecurityDescriptorFromString("O:" + sid.String() + "D:P(A;OICI;FA;;;" + sid.String() + ")(A;OICI;FA;;;SY)")
	return sid, sd, err
}

func checkPrivate(h windows.Handle, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !windows.EqualSid(owner, user.User.Sid) {
		return ErrUnsafe
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if directory && control&windows.SE_DACL_PROTECTED == 0 {
		return ErrUnsafe
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil || acl.AceCount != 2 {
		return ErrUnsafe
	}
	own, sys := false, false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var header *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &header); err != nil {
			return err
		}
		if header.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || header.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || header.Mask&0x1f01ff != 0x1f01ff {
			return ErrUnsafe
		}
		sid := (*windows.SID)(unsafe.Pointer(&header.SidStart))
		if windows.EqualSid(sid, user.User.Sid) {
			own = true
		} else if windows.EqualSid(sid, system) {
			sys = true
		} else {
			return ErrUnsafe
		}
	}
	if !own || !sys {
		return ErrUnsafe
	}
	return nil
}

func guardDirectory(path, anchor string, create bool) (guard *directoryGuard, err error) {
	guard = &directoryGuard{}
	defer func(g *directoryGuard) {
		if err != nil {
			_ = g.close()
		}
	}(guard)
	current := filepath.VolumeName(path) + `\`
	parts := strings.Split(strings.TrimPrefix(path, current), `\`)
	// Every ancestor remains open without FILE_SHARE_DELETE, including public
	// ancestors above the private root. Checking only the leaf would permit a
	// junction/rename race in a previously checked parent.
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
			_, sd, e := userSecurity()
			if e != nil {
				return nil, e
			}
			sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
			if e = windows.CreateDirectory(ptr, &sa); e != nil && !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
				return nil, e
			}
		}
		h, e := windows.CreateFile(ptr, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if e != nil {
			return nil, e
		}
		guard.handles = append(guard.handles, h)
		var info windows.ByHandleFileInformation
		if e = windows.GetFileInformationByHandle(h, &info); e != nil {
			return nil, e
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return nil, ErrUnsafe
		}
		if i == -1 {
			fs := make([]uint16, 32)
			if e = windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); e != nil {
				return nil, e
			}
			drive, e := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
			if e != nil {
				return nil, e
			}
			if windows.UTF16ToString(fs) != "NTFS" || windows.GetDriveType(drive) == windows.DRIVE_REMOTE {
				return nil, ErrUnsafe
			}
		}
		if current == anchor || strings.HasPrefix(current, anchor+`\`) {
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
	_, sd, err := userSecurity()
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := windows.CreateFile(ptr, access, share, &sa, disposition, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	fail := func(e error) (*os.File, error) { _ = windows.CloseHandle(h); return nil, e }
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		return fail(err)
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || info.NumberOfLinks != 1 {
		return fail(ErrUnsafe)
	}
	if err = checkPrivate(h, false); err != nil {
		return fail(err)
	}
	f := os.NewFile(uintptr(h), path)
	return f, nil
}

func publish(ctx context.Context, from, to string, replace bool) error {
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

func handlePath(h windows.Handle) (string, error) {
	buffer := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), 0)
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
		return ErrUnsafe
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
