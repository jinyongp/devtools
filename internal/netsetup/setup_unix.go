//go:build !windows

// Package netsetup manages explicit low-port permission intent for the installed
// executable. The daemon always attempts the actual bind independently of intent.
package netsetup

import (
	"crypto/sha256"
	"encoding/hex"

	"io"
	"os"
	"path/filepath"

	"syscall"

	"github.com/jinyongp/devtools/internal/platformfs"

	"github.com/jinyongp/devtools/internal/protocol"
)

func OpenIdentity(path string) (*os.File, Identity, *protocol.Error) {
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil {
		return nil, Identity{}, failure("executable_unavailable")
	}
	canonical, e = filepath.Abs(canonical)
	if e != nil {
		return nil, Identity{}, failure("executable_unavailable")
	}
	f, e := platformfs.OpenNoFollow(canonical, os.O_RDONLY, 0)
	if e != nil {
		return nil, Identity{}, failure("executable_unavailable")
	}
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0002 != 0 {
		f.Close()
		return nil, Identity{}, failure("unsafe_executable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		f.Close()
		return nil, Identity{}, failure("unsafe_executable")
	}
	hash := sha256.New()
	if _, e := io.Copy(hash, f); e != nil {
		f.Close()
		return nil, Identity{}, failure("executable_unavailable")
	}
	_, _ = f.Seek(0, io.SeekStart)
	return f, Identity{canonical, uint64(stat.Dev), uint64(stat.Ino), hex.EncodeToString(hash.Sum(nil))}, nil
}
