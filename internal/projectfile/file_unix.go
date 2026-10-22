//go:build linux || darwin

package projectfile

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func attributes(f *os.File) (map[string][]byte, error) {
	n, err := unix.Flistxattr(int(f.Fd()), nil)
	if err == unix.ENOTSUP {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	n, err = unix.Flistxattr(int(f.Fd()), buf)
	if err != nil {
		return nil, err
	}
	attrs := map[string][]byte{}
	for _, name := range strings.Split(string(buf[:n]), "\x00") {
		if name == "" {
			continue
		}
		n, err := unix.Fgetxattr(int(f.Fd()), name, nil)
		if err != nil {
			return nil, err
		}
		v := make([]byte, n)
		n, err = unix.Fgetxattr(int(f.Fd()), name, v)
		if err != nil {
			return nil, err
		}
		attrs[name] = v[:n]
	}
	return attrs, nil
}
func metadata(f *os.File, info os.FileInfo) (string, string, error) {
	st := info.Sys().(*syscall.Stat_t)
	attrs, err := attributes(f)
	if err != nil {
		return "", "", err
	}
	keys := []string{}
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	security := fmt.Sprintf("%d:%d:%d", st.Uid, st.Gid, info.Mode())
	acl, err := extendedACL(f)
	if err != nil {
		return "", "", err
	}
	security += "|acl:" + hex.EncodeToString([]byte(acl))
	for _, k := range keys {
		security += "|" + hex.EncodeToString([]byte(k)) + ":" + hex.EncodeToString(attrs[k])
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), security, nil
}
func safeTarget(s *Snapshot) error {
	i, err := os.Lstat(s.Path)
	if err != nil {
		return err
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok || !i.Mode().IsRegular() || st.Nlink != 1 || i.Mode().Perm()&0222 == 0 || !os.SameFile(i, s.Info) {
		return os.ErrPermission
	}
	return nil
}
func copyMetadata(s *Snapshot, dst *os.File) error {
	src, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer src.Close()
	i, err := src.Stat()
	if err != nil || !os.SameFile(i, s.Info) {
		return os.ErrInvalid
	}
	st := s.Info.Sys().(*syscall.Stat_t)
	di, err := dst.Stat()
	if err != nil {
		return err
	}
	ds := di.Sys().(*syscall.Stat_t)
	if ds.Uid != st.Uid || ds.Gid != st.Gid {
		if err = dst.Chown(int(st.Uid), int(st.Gid)); err != nil {
			return err
		}
	}
	if err = dst.Chmod(s.Info.Mode()); err != nil {
		return err
	}
	attrs, err := attributes(src)
	if err != nil {
		return err
	}
	for name, value := range attrs {
		if err = unix.Fsetxattr(int(dst.Fd()), name, value, 0); err != nil {
			return err
		}
	}
	return copyExtendedACL(src, dst)
}
func replace(_ context.Context, s, candidate *Snapshot) (bool, bool, string, error) {
	err := os.Rename(candidate.Path, s.Path)
	if err != nil {
		if now, e := Read(s.Path); e == nil && now.Revision == s.Revision {
			return false, false, "", err
		}
		return false, true, "", err
	}
	dir, err := os.Open(filepath.Dir(s.Path))
	if err != nil {
		return true, false, "", err
	}
	defer dir.Close()
	err = dir.Sync()
	return true, false, "", err
}
