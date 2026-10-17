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
func replaceHostsFile(ctx context.Context, original *platformfs.File, body []byte) error {
	handle := windows.Handle(original.Fd())
	security, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.SACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	expectedSecurity := security.String()
	if expectedSecurity == "" {
		return platformfs.ErrUnsafe
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
	fileHandle, err := windows.CreateFile(ptr, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.ACCESS_SYSTEM_SECURITY, windows.FILE_SHARE_READ, &attributes, windows.CREATE_NEW, info.FileAttributes|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
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
	actual, err := windows.GetSecurityInfo(fileHandle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.SACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if actual.String() != expectedSecurity {
		return platformfs.ErrUnsafe
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(`\\?\` + path)
	if err != nil {
		return err
	}
	deadline, delay := time.Now().Add(2*time.Second), 10*time.Millisecond
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = sameHostsFile(handle, path); err != nil {
			return err
		}
		err = windows.MoveFileEx(ptr, destination, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
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
