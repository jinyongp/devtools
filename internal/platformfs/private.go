// Package platformfs implements private local storage at the operating system
// boundary. Logical journals, locking order, and recovery remain with callers.
package platformfs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

var ErrUnsafe = errors.New("private local storage required")

// Root pins a checked private directory. Close it after closing its files.
// Opening a root never changes the permissions of existing objects.
type Root struct {
	path   string
	guard  *directoryGuard
	closed atomic.Bool
}

// File retains checked parent directories until the file handle is closed.
// Use Close on this wrapper, rather than closing its embedded File directly.
type File struct {
	*os.File
	guard    *directoryGuard
	once     sync.Once
	closeErr error
}

func (f *File) Close() error {
	f.once.Do(func() { f.closeErr = errors.Join(f.File.Close(), f.guard.close()) })
	return f.closeErr
}

// ValidatePrivatePath checks a root's path syntax without creating or opening it.
// Filesystem type, ACL and reparse checks happen when OpenRoot acquires handles.
func ValidatePrivatePath(path string) (string, error) { return privatePath(path) }

// OpenRoot opens an existing root, or creates missing directories with private
// permissions when create is true. Existing public roots are rejected.
func OpenRoot(path string, create bool) (*Root, error) {
	absolute, err := privatePath(path)
	if err != nil {
		return nil, err
	}
	guard, err := guardDirectory(absolute, absolute, create)
	if err != nil {
		return nil, err
	}
	return &Root{path: absolute, guard: guard}, nil
}

func (r *Root) Close() error {
	if r.closed.Swap(true) {
		return nil
	}
	return r.guard.close()
}

func (r *Root) child(name string) (string, error) {
	if r.closed.Load() {
		return "", os.ErrClosed
	}
	if !filepath.IsLocal(name) {
		return "", fsError("validate_path", r.path, "invalid_path", fmt.Errorf("%w: relative child path required", ErrUnsafe))
	}
	path := filepath.Join(r.path, name)
	if _, err := privatePath(path); err != nil {
		return "", err
	}
	return path, nil
}

// MkdirAll creates private directories below the root. It rejects aliases and
// insecure existing directories instead of silently repairing their permissions.
func (r *Root) MkdirAll(name string) error {
	path, err := r.child(name)
	if err != nil {
		return err
	}
	guard, err := guardDirectory(path, r.path, true)
	if err != nil {
		return err
	}
	return guard.close()
}

// OpenFile checks the parent chain and file security before truncating data.
func (r *Root) OpenFile(name string, flags int) (*File, error) {
	return r.openFile(name, flags, openPrivateFile)
}

func (r *Root) openFile(name string, flags int, open func(string, int) (*os.File, error)) (*File, error) {
	path, err := r.child(name)
	if err != nil {
		return nil, err
	}
	if path == r.path {
		return nil, fsError("validate_path", path, "invalid_path", ErrUnsafe)
	}
	guard, err := guardDirectory(filepath.Dir(path), r.path, false)
	if err != nil {
		return nil, err
	}
	f, err := open(path, flags)
	if err != nil {
		_ = guard.close()
		return nil, err
	}
	if err = containedFile(r, f); err == nil && flags&os.O_TRUNC != 0 {
		err = f.Truncate(0)
	}
	if err != nil {
		_ = f.Close()
		_ = guard.close()
		return nil, err
	}
	return &File{File: f, guard: guard}, nil
}

func (r *Root) ReadFile(name string, limit int64) (body []byte, err error) {
	defer func() { err = fsError("read_file", name, "", err) }()
	if limit < 0 || limit == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("invalid read limit")
	}
	f, err := r.OpenFile(name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fsError("read_file", name, "size_limit", fmt.Errorf("private file exceeds read limit"))
	}
	body, err = io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(body)) > limit {
		return nil, fsError("read_file", name, "size_limit", fmt.Errorf("private file exceeds read limit"))
	}
	return body, err
}

// WriteFile flushes and closes a same-directory private temp before publishing.
// replace=false never overwrites an existing target. Callers still own their
// journal's before-image and phase transitions; directory durability differs by OS.
func (r *Root) WriteFile(ctx context.Context, name string, body []byte, replace bool) (err error) {
	defer func() { err = fsError("write_file", name, "", err) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := r.child(name)
	if err != nil {
		return err
	}
	parent, err := guardDirectory(filepath.Dir(path), r.path, false)
	if err != nil {
		return err
	}
	defer parent.close()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(name), ".write-"+hex.EncodeToString(random[:]))
	f, err := r.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	tempPath := f.Name()
	defer os.Remove(tempPath)
	_, err = f.Write(body)
	err = fsError("write_file", path, "", err)
	if err == nil {
		err = fsError("sync_file", path, "", f.Sync())
	}
	err = errors.Join(err, fsError("close_file", path, "", f.Close()))
	if err != nil {
		return err
	}
	return publish(ctx, tempPath, path, replace)
}
