package diagnostics

import (
	"golang.org/x/sys/unix"
	"strings"
)

func storageFilesystem(path string) (string, bool, bool) {
	parent, err := existingPlatformAncestor(path)
	if err != nil {
		return "unknown", false, false
	}
	var stat unix.Statfs_t
	if unix.Statfs(parent, &stat) != nil {
		return "unknown", false, false
	}
	name := strings.TrimRight(string(stat.Fstypename[:]), "\x00")
	switch name {
	case "apfs", "hfs":
		return name, true, true
	case "smbfs", "nfs":
		return name, false, true
	}
	return "unknown", false, false
}
