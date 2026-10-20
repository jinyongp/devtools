package diagnostics

import (
	"golang.org/x/sys/windows"
	"strings"
	"unsafe"
)

func platformIsolation() string {
	// TokenIsAppContainer (29) is defined by the Windows SDK but is absent from
	// x/sys's TOKEN_INFORMATION_CLASS constants. Inspect the process, not its path.
	var appContainer, size uint32
	token := windows.GetCurrentProcessToken()
	if windows.GetTokenInformation(token, 29, (*byte)(unsafe.Pointer(&appContainer)), uint32(unsafe.Sizeof(appContainer)), &size) != nil {
		return "unknown"
	}
	if appContainer != 0 {
		return "appcontainer"
	}
	restricted, err := token.IsRestricted()
	if err != nil {
		return "unknown"
	}
	if restricted {
		return "restricted"
	}
	return "unrestricted"
}

func storageFilesystem(path string) (string, bool, bool) {
	parent, err := existingPlatformAncestor(path)
	if err != nil {
		return "unknown", false, false
	}
	pointer, err := windows.UTF16PtrFromString(parent)
	if err != nil {
		return "unknown", false, false
	}
	volume := make([]uint16, windows.MAX_PATH+1)
	if windows.GetVolumePathName(pointer, &volume[0], uint32(len(volume))) != nil {
		return "unknown", false, false
	}
	if windows.GetDriveType(&volume[0]) == windows.DRIVE_REMOTE {
		return "smb", false, true
	}
	fs := make([]uint16, 64)
	if windows.GetVolumeInformation(&volume[0], nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))) != nil {
		return "unknown", false, false
	}
	name := strings.ToLower(windows.UTF16ToString(fs))
	return name, name == "ntfs", true
}
