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
	"time"

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
	return readHosts(f)
}

func (s System) observe() ([]byte, bool, *protocol.Error) {
	f, err := platformfs.OpenNoFollow(s.path(), os.O_RDONLY, 0)
	if err != nil {
		return nil, false, failure("hosts_unavailable")
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if err != nil && !errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, false, failure("hosts_unavailable")
	}
	b, e := readHosts(f)
	held, heldErr := f.Stat()
	named, nameErr := os.Lstat(s.path())
	if heldErr != nil || nameErr != nil || !os.SameFile(held, named) {
		return nil, false, failure("hosts_unavailable")
	}
	return b, err != nil, e
}

func readHosts(f *os.File) ([]byte, *protocol.Error) {
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return nil, failure("hosts_unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(f, maxHostsSize+1))
	if e != nil || len(b) > maxHostsSize {
		return nil, failure("hosts_unavailable")
	}
	return b, nil
}

func (s System) Apply(ctx context.Context, q Request) *protocol.Error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	original, err := platformfs.OpenNoFollow(s.path(), os.O_RDONLY, 0)
	if err != nil {
		return failure("hosts_unavailable")
	}
	defer original.Close()
	tick := time.NewTicker(15 * time.Millisecond)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return failure("canceled")
		}
		err = syscall.Flock(int(original.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			return failure("hosts_unavailable")
		}
		select {
		case <-ctx.Done():
			return failure("canceled")
		case <-tick.C:
		}
	}
	info, err := original.Stat()
	named, nameErr := os.Lstat(s.path())
	if err != nil || nameErr != nil || !os.SameFile(info, named) {
		return failure("stale_preview")
	}
	before, e := readHosts(original)
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
	if _, err := original.Seek(0, io.SeekStart); err != nil {
		return failure("hosts_unavailable")
	}
	current, e := readHosts(original)
	if e != nil {
		return e
	}
	currentInfo, err := os.Lstat(s.path())
	if err != nil || !os.SameFile(info, currentInfo) || !bytes.Equal(current, before) {
		return failure("stale_preview")
	}
	if ctx.Err() != nil {
		return failure("canceled")
	}
	if err = os.Rename(name, s.path()); err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EBUSY) {
		return failure("hosts_unavailable")
	}
	// WSL and containers can mount /etc/hosts as a single file. Keep its
	// inode and metadata. The original descriptor already holds the lock.
	file, err := platformfs.OpenNoFollow(s.path(), os.O_RDWR, 0)
	if err != nil {
		return failure("permission_required")
	}
	defer file.Close()
	writable, err := file.Stat()
	named, nameErr = os.Lstat(s.path())
	if err != nil || nameErr != nil || !os.SameFile(info, writable) || !os.SameFile(info, named) {
		return failure("stale_preview")
	}
	current, e = readHosts(file)
	if e != nil || !bytes.Equal(current, before) {
		return failure("stale_preview")
	}
	if ctx.Err() != nil {
		return failure("canceled")
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
