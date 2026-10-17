package netsetup

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"strings"
)

// OpenIdentity reads a local NTFS image through a held, non-writable handle.
// Native mode never changes capabilities, firewall rules or reserved ports.
func OpenIdentity(path string) (*os.File, Identity, *protocol.Error) {
	canonical, err := location.Canonical(path)
	if err != nil {
		return nil, Identity{}, failure("executable_unavailable")
	}
	ptr, err := windows.UTF16PtrFromString(`\\?\` + canonical)
	if err != nil {
		return nil, Identity{}, failure("executable_unavailable")
	}
	h, err := windows.CreateFile(ptr, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, Identity{}, failure("executable_unavailable")
	}
	f := os.NewFile(uintptr(h), canonical)
	fail := func(code string) (*os.File, Identity, *protocol.Error) {
		f.Close()
		return nil, Identity{}, failure(code)
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return fail("unsafe_executable")
	}
	fs := make([]uint16, 32)
	if err = windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))); err != nil || windows.UTF16ToString(fs) != "NTFS" {
		return fail("unsafe_executable")
	}
	// The final handle path is authoritative if the named parent changed during
	// canonicalization; device/UNC paths remain outside the supported contract.
	buffer := make([]uint16, 512)
	for {
		n, e := windows.GetFinalPathNameByHandle(h, &buffer[0], uint32(len(buffer)), 0)
		if e != nil {
			return fail("executable_unavailable")
		}
		if n >= uint32(len(buffer)) {
			buffer = make([]uint16, n+1)
			continue
		}
		canonical = strings.TrimPrefix(windows.UTF16ToString(buffer[:n]), `\\?\`)
		break
	}
	if strings.HasPrefix(canonical, `UNC\`) {
		return fail("unsafe_executable")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return fail("executable_unavailable")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return fail("executable_unavailable")
	}
	return f, Identity{Path: canonical, Device: uint64(info.VolumeSerialNumber), Inode: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow), Digest: hex.EncodeToString(hash.Sum(nil))}, nil
}

func setupIdentityPath(path string) (string, error) { return location.Canonical(path) }
