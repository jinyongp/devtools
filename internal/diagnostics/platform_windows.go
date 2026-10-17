package diagnostics

import (
	"golang.org/x/sys/windows"
	"strings"
)

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
