package projectfile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const securityFields = windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION

func metadata(f *os.File, info os.FileInfo) (string, string, error) {
	var id windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &id); err != nil {
		return "", "", err
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, securityFields)
	if err != nil {
		return "", "", err
	}
	security, err := comparableSecurity(sd)
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("%d:%d:%d", id.VolumeSerialNumber, id.FileIndexHigh, id.FileIndexLow), fmt.Sprintf("%s:%d", security, id.FileAttributes&windows.FILE_ATTRIBUTE_READONLY), nil
}

// Windows may clear AUTO_INHERITED when assigning a descriptor to a regular
// file. It records propagation to children, not the inherited flag on each ACE.
// Keep every ACE, owner/group and protection flag in the revision comparison.
func comparableSecurity(sd *windows.SECURITY_DESCRIPTOR) (string, error) {
	value := sd.String()
	if value == "" {
		return "", os.ErrInvalid
	}
	copy, err := windows.SecurityDescriptorFromString(value)
	if err != nil {
		return "", err
	}
	if err = copy.SetControl(windows.SE_DACL_AUTO_INHERITED, 0); err != nil {
		return "", err
	}
	value = copy.String()
	if value == "" {
		return "", os.ErrInvalid
	}
	return value, nil
}
func safeTarget(s *Snapshot) error {
	i, e := os.Lstat(s.Path)
	if e != nil {
		return e
	}
	if !i.Mode().IsRegular() || !os.SameFile(i, s.Info) {
		return os.ErrPermission
	}
	f, e := os.Open(s.Path)
	if e != nil {
		return e
	}
	defer f.Close()
	var id windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &id); e != nil {
		return e
	}
	if id.NumberOfLinks != 1 || id.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_READONLY) != 0 {
		return os.ErrPermission
	}
	return nil
}
func copyMetadata(s *Snapshot, dst *os.File) error {
	src, e := os.Open(s.Path)
	if e != nil {
		return e
	}
	defer src.Close()
	i, e := src.Stat()
	if e != nil || !os.SameFile(i, s.Info) {
		return os.ErrInvalid
	}
	sd, e := windows.GetSecurityInfo(windows.Handle(src.Fd()), windows.SE_FILE_OBJECT, securityFields)
	if e != nil {
		return e
	}
	owner, _, e := sd.Owner()
	if e != nil {
		return e
	}
	group, _, e := sd.Group()
	if e != nil {
		return e
	}
	dacl, _, e := sd.DACL()
	if e != nil {
		return e
	}
	// os.CreateTemp grants data access, not WRITE_DAC/WRITE_OWNER. Reopen the
	// same kernel object with the security rights needed for this copy.
	current, e := windows.GetSecurityInfo(windows.Handle(dst.Fd()), windows.SE_FILE_OBJECT, securityFields)
	if e != nil {
		return e
	}
	currentOwner, _, e := current.Owner()
	if e != nil {
		return e
	}
	currentGroup, _, e := current.Group()
	if e != nil {
		return e
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	access := uint32(windows.READ_CONTROL | windows.WRITE_DAC)
	if owner.String() != currentOwner.String() {
		flags |= windows.OWNER_SECURITY_INFORMATION
		access |= windows.WRITE_OWNER
	}
	if group.String() != currentGroup.String() {
		flags |= windows.GROUP_SECURITY_INFORMATION
		access |= windows.WRITE_OWNER
	}
	control, _, e := sd.Control()
	if e != nil {
		return e
	}
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		flags |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	handle, _, reopenErr := reOpenFile.Call(dst.Fd(), uintptr(access), windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, 0)
	if windows.Handle(handle) == windows.InvalidHandle {
		return reopenErr
	}
	defer windows.CloseHandle(windows.Handle(handle))
	return windows.SetSecurityInfo(windows.Handle(handle), windows.SE_FILE_OBJECT, flags, owner, group, dacl, nil)
}

var replaceFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")
var reOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

func replace(ctx context.Context, s, candidate *Snapshot) (bool, bool, string, error) {
	source, err := os.Open(s.Path)
	if err != nil {
		return false, false, "", err
	}
	info, err := source.Stat()
	if err != nil || !os.SameFile(info, s.Info) {
		source.Close()
		return false, false, "", Error("config_revision_conflict", "target_changed", err)
	}
	security, err := windows.GetSecurityInfo(windows.Handle(source.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	source.Close()
	if err != nil {
		return false, false, "", err
	}
	dacl, _, err := security.DACL()
	if err != nil {
		return false, false, "", err
	}
	control, _, err := security.Control()
	if err != nil {
		return false, false, "", err
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		flags |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	name, err := windows.UTF16PtrFromString(candidate.Path)
	if err != nil {
		return false, false, "", err
	}
	// Pin the replacement object with metadata rights and share-delete. Any
	// post-replacement DACL repair must affect this object, never a later writer
	// that has taken over the published pathname.
	handle, err := windows.CreateFile(name, windows.READ_CONTROL|windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return false, false, "", err
	}
	defer windows.CloseHandle(handle)
	var pinned windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &pinned); err != nil {
		return false, false, "", err
	}
	if fmt.Sprintf("%d:%d:%d", pinned.VolumeSerialNumber, pinned.FileIndexHigh, pinned.FileIndexLow) != candidate.identity {
		return false, false, "", Error("config_revision_conflict", "target_changed", nil)
	}
	a, _ := windows.UTF16PtrFromString(s.Path)
	b, _ := windows.UTF16PtrFromString(candidate.Path)
	backup := candidate.Path + ".backup"
	c, _ := windows.UTF16PtrFromString(backup)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if e := s.Recheck(); e != nil {
			return false, false, "", e
		}
		if e := candidate.Recheck(); e != nil {
			return false, false, "", e
		}
		result, _, e := replaceFile.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), uintptr(unsafe.Pointer(c)), 0, 0, 0)
		if result != 0 {
			// ReplaceFileW converts inherited ACEs to explicit entries and then
			// propagates parent ACEs again. Restore the captured DACL and protection
			// policy through the pinned object before observing publication.
			err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, flags, nil, nil, dacl, nil)
			runtime.KeepAlive(security)
			return true, false, backup, err
		}
		if errors.Is(e, windows.ERROR_SHARING_VIOLATION) && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return false, false, "", ctx.Err()
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}
		if now, err := Read(s.Path); err == nil && now.Revision == s.Revision {
			return false, false, "", e
		}
		return false, true, backup, e
	}
}
