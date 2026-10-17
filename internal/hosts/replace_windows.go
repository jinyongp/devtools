package hosts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

var hostsPrivilegeMu sync.Mutex

func enableHostsMetadataPrivileges() (func(), error) {
	hostsPrivilegeMu.Lock()
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		hostsPrivilegeMu.Unlock()
		return nil, err
	}
	previous := []windows.Tokenprivileges{}
	restore := func() {
		for i := len(previous) - 1; i >= 0; i-- {
			_ = windows.AdjustTokenPrivileges(token, false, &previous[i], 0, nil, nil)
		}
		token.Close()
		hostsPrivilegeMu.Unlock()
	}
	proc := windows.NewLazySystemDLL("advapi32.dll").NewProc("AdjustTokenPrivileges")
	for _, name := range []string{"SeRestorePrivilege", "SeSecurityPrivilege"} {
		var luid windows.LUID
		ptr, err := windows.UTF16PtrFromString(name)
		if err != nil {
			restore()
			return nil, err
		}
		if err = windows.LookupPrivilegeValue(nil, ptr, &luid); err != nil {
			restore()
			return nil, err
		}
		state := windows.Tokenprivileges{PrivilegeCount: 1, Privileges: [1]windows.LUIDAndAttributes{{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}}}
		var old windows.Tokenprivileges
		var size uint32
		result, _, callErr := proc.Call(uintptr(token), 0, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(old), uintptr(unsafe.Pointer(&old)), uintptr(unsafe.Pointer(&size)))
		if result == 0 || errors.Is(callErr, windows.ERROR_NOT_ALL_ASSIGNED) {
			restore()
			return nil, callErr
		}
		previous = append(previous, old)
	}
	return restore, nil
}
func sameHostsFile(handle windows.Handle, path string) error {
	ptr, err := windows.UTF16PtrFromString(`\\?\` + path)
	if err != nil {
		return err
	}
	named, err := windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(named)
	var held, actual windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &held); err != nil {
		return err
	}
	if err = windows.GetFileInformationByHandle(named, &actual); err != nil {
		return err
	}
	if actual.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 || actual.NumberOfLinks != 1 || held.VolumeSerialNumber != actual.VolumeSerialNumber || held.FileIndexHigh != actual.FileIndexHigh || held.FileIndexLow != actual.FileIndexLow {
		return platformfs.ErrUnsafe
	}
	return nil
}

// On a regular hosts file the AUTO_INHERITED control bits describe propagation
// to child objects, which a regular file cannot have. Windows clears them when
// assigning a descriptor. Compare every ACE (including inherited ACE flags),
// owner/group, SACL and protection flag; normalize only those two control bits.
func comparableHostsSecurity(sd *windows.SECURITY_DESCRIPTOR) (string, error) {
	value := sd.String()
	if value == "" {
		return "", platformfs.ErrUnsafe
	}
	copy, err := windows.SecurityDescriptorFromString(value)
	if err != nil {
		return "", err
	}
	if err = copy.SetControl(windows.SE_DACL_AUTO_INHERITED|windows.SE_SACL_AUTO_INHERITED, 0); err != nil {
		return "", err
	}
	value = copy.String()
	if value == "" {
		return "", platformfs.ErrUnsafe
	}
	return value, nil
}

func replaceHostsFile(ctx context.Context, original *platformfs.File, body []byte) error {
	handle := windows.Handle(original.Fd())
	security, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.SACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	expectedSecurity, err := comparableHostsSecurity(security)
	if err != nil {
		return err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	path := original.Name()
	candidate := filepath.Join(filepath.Dir(path), ".devtools-hosts-"+hex.EncodeToString(random[:])+".tmp")
	ptr, err := windows.UTF16PtrFromString(`\\?\` + candidate)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: security}
	fileHandle, err := windows.CreateFile(ptr, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC|windows.WRITE_OWNER|windows.ACCESS_SYSTEM_SECURITY, windows.FILE_SHARE_READ, &attributes, windows.CREATE_NEW, info.FileAttributes|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	runtime.KeepAlive(security)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fileHandle), candidate)
	defer func() { file.Close(); _ = windows.DeleteFile(ptr) }()
	if _, err = file.Write(body); err != nil {
		return err
	}
	if err = windows.SetFileTime(fileHandle, &info.CreationTime, &info.LastAccessTime, nil); err != nil {
		return err
	}
	control, _, err := security.Control()
	if err != nil {
		return err
	}
	information := windows.SECURITY_INFORMATION(windows.OWNER_SECURITY_INFORMATION | windows.GROUP_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		information |= windows.PROTECTED_DACL_SECURITY_INFORMATION
	} else {
		information |= windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	}
	if control&windows.SE_SACL_PRESENT != 0 {
		information |= windows.SACL_SECURITY_INFORMATION
		if control&windows.SE_SACL_PROTECTED != 0 {
			information |= windows.PROTECTED_SACL_SECURITY_INFORMATION
		} else {
			information |= windows.UNPROTECTED_SACL_SECURITY_INFORMATION
		}
	}
	if err = windows.SetKernelObjectSecurity(fileHandle, information, security); err != nil {
		return err
	}
	actual, err := windows.GetSecurityInfo(fileHandle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.SACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	actualSecurity, err := comparableHostsSecurity(actual)
	if err != nil {
		return err
	}
	if actualSecurity != expectedSecurity {
		return platformfs.ErrUnsafe
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}

	// MoveFileEx(REPLACE_EXISTING) rejects an open destination on these native
	// runners. Go Root.Rename uses NTFS POSIX replacement semantics, preserving
	// the held original handle (and write exclusion) until publication completes.
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	deadline, delay := time.Now().Add(2*time.Second), 10*time.Millisecond
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = sameHostsFile(handle, path); err != nil {
			return err
		}
		err = root.Rename(filepath.Base(candidate), filepath.Base(path))
		if err == nil || (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED)) {
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
