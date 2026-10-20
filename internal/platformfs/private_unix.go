//go:build linux || darwin

package platformfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type directoryGuard struct{ files []*os.File }

func (g *directoryGuard) close() error {
	var err error
	for i := len(g.files) - 1; i >= 0; i-- {
		err = errors.Join(err, g.files[i].Close())
	}
	return err
}
func privatePath(path string) (string, error) {
	if path == "" {
		return "", fsError("validate_path", path, "invalid_path", ErrUnsafe)
	}
	return filepath.Abs(path)
}
func guardDirectory(path, anchor string, create bool) (guard *directoryGuard, err error) {
	defer func() { err = fsError("open_directory", path, "", err) }()
	guard = &directoryGuard{}
	defer func(g *directoryGuard) {
		if err != nil {
			_ = g.close()
		}
	}(guard)
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), current) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if create {
			if e := os.Mkdir(current, 0700); e != nil && !errors.Is(e, os.ErrExist) {
				return nil, fsError("create_directory", current, "", e)
			}
		}
		f, e := os.OpenFile(current, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return nil, fsError("open_directory", current, "", e)
		}
		guard.files = append(guard.files, f)
		info, e := f.Stat()
		if e != nil {
			return nil, e
		}
		if !info.IsDir() {
			return nil, fsError("inspect_directory", current, "not_directory", ErrUnsafe)
		}
		if current == anchor || strings.HasPrefix(current, anchor+string(filepath.Separator)) {
			if info.Mode().Perm()&0077 != 0 {
				return nil, securityError(true, "public_permissions", ErrUnsafe)
			}
		}
	}
	return guard, nil
}
func openPrivateFile(path string, flags int) (*os.File, error) {
	// Never truncate before validating the object. O_NONBLOCK prevents FIFO opens
	// from waiting before the regular-file check.
	f, err := os.OpenFile(path, (flags&^os.O_TRUNC)|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, fsError("open_file", path, "", err)
	}
	fail := func(e error) (*os.File, error) { _ = f.Close(); return nil, e }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fail(securityError(false, "public_or_non_regular_file", ErrUnsafe))
	}
	return f, nil
}
func publish(ctx context.Context, from, to string, replace bool) (err error) {
	defer func() { err = fsError("publish_file", to, "", err) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if replace {
		if info, err := os.Lstat(to); err == nil {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return fsError("inspect_file", to, "public_or_non_regular_file", ErrUnsafe)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
	} else {
		if err := os.Link(from, to); err != nil {
			return err
		}
		if err := os.Remove(from); err != nil {
			return err
		}
	}
	dir, err := os.Open(filepath.Dir(to))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func containedFile(r *Root, f *os.File) error {
	for _, parent := range r.guard.files {
		held, err := parent.Stat()
		if err != nil {
			return err
		}
		named, err := os.Lstat(parent.Name())
		if err != nil {
			return err
		}
		if !os.SameFile(held, named) {
			return fsError("verify_directory_identity", parent.Name(), "path_mismatch", ErrUnsafe)
		}
	}
	held, err := f.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(f.Name())
	if err != nil {
		return err
	}
	if !os.SameFile(held, named) {
		return fsError("verify_file_identity", f.Name(), "path_mismatch", ErrUnsafe)
	}
	return nil
}
