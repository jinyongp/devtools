package diagnostics

import "golang.org/x/sys/unix"

func storageFilesystem(path string) (string, bool, bool) {
	parent, err := existingPlatformAncestor(path)
	if err != nil {
		return "unknown", false, false
	}
	var stat unix.Statfs_t
	if unix.Statfs(parent, &stat) != nil {
		return "unknown", false, false
	}
	return linuxFilesystem(uint64(stat.Type))
}
func linuxFilesystem(kind uint64) (string, bool, bool) {
	switch kind {
	case 0xef53:
		return "ext", true, true
	case 0x01021994:
		return "tmpfs", true, true
	case 0x9123683e:
		return "btrfs", true, true
	case 0x58465342:
		return "xfs", true, true
	case 0x794c7630:
		return "overlay", true, true
	case 0x01021997:
		return "9p", false, true
	case 0x5346544e:
		return "ntfs", false, true
	case 0xff534d42, 0x517b:
		return "smb", false, true
	case 0x6969:
		return "nfs", false, true
	default:
		return "unknown", false, false
	}
}
