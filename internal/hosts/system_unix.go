//go:build !windows

package hosts

import (
	"bytes"
	"context"

	"errors"
	"io"

	"os"
	"path/filepath"

	"syscall"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s System) path() string {
	if s.Path != "" {
		return s.Path
	}
	return "/etc/hosts"
}
func (s System) Read() ([]byte, *protocol.Error) {
	f, e := platformfs.OpenNoFollow(s.path(), os.O_RDONLY, 0)
	if e != nil {
		return nil, failure("hosts_unavailable")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return nil, failure("hosts_unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if e != nil || len(b) > 1<<20 {
		return nil, failure("hosts_unavailable")
	}
	return b, nil
}

func (s System) Apply(ctx context.Context, q Request) *protocol.Error {
	if ctx.Err() != nil {
		return failure("canceled")
	}
	before, e := s.Read()
	if e != nil {
		return e
	}
	if digest(before) != q.Expected {
		return failure("stale_preview")
	}
	after, e := Render(before, q.Mappings)
	if e != nil {
		return e
	}
	if bytes.Equal(before, after) {
		return nil
	}
	info, err := os.Lstat(s.path())
	if err != nil || !info.Mode().IsRegular() {
		return failure("hosts_unavailable")
	}
	f, err := os.CreateTemp(filepath.Dir(s.path()), ".devtools-hosts-*")
	if err != nil {
		return failure("permission_required")
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if err = f.Chmod(info.Mode().Perm()); err != nil {
		return failure("hosts_unavailable")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err = f.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
			return failure("hosts_unavailable")
		}
	}
	if _, err = f.Write(after); err != nil {
		return failure("hosts_unavailable")
	}
	if f.Sync() != nil || f.Close() != nil {
		return failure("hosts_unavailable")
	}
	current, e := s.Read()
	if e != nil {
		return e
	}
	currentInfo, err := os.Lstat(s.path())
	if err != nil || !os.SameFile(info, currentInfo) || !bytes.Equal(current, before) {
		return failure("stale_preview")
	}
	if err = os.Rename(name, s.path()); err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EBUSY) {
		return failure("hosts_unavailable")
	}
	// WSL and containers can mount /etc/hosts as a single file. Keep its
	// inode and metadata, lock the opened file, and recheck the frozen digest.
	file, err := platformfs.OpenNoFollow(s.path(), os.O_RDWR, 0)
	if err != nil {
		return failure("permission_required")
	}
	defer file.Close()
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX) != nil {
		return failure("hosts_unavailable")
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	current, err = io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || !bytes.Equal(current, before) {
		return failure("stale_preview")
	}
	if _, err = file.WriteAt(after, 0); err != nil {
		_, _ = file.WriteAt(before, 0)
		_ = file.Truncate(int64(len(before)))
		return failure("recovery_required")
	}
	if file.Truncate(int64(len(after))) != nil || file.Sync() != nil {
		return failure("recovery_required")
	}
	return nil
}
