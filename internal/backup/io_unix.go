//go:build !windows

package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func ensurePrivateDirectoryChain(root, directory string) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(directory) {
		return errors.New("absolute private directory required")
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return errors.New("private directory must be below data root")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	current := root
	components := append([]string{"."}, strings.Split(relative, string(os.PathSeparator))...)
	for index, component := range components {
		created := false
		parent := current
		if index > 0 {
			current = filepath.Join(current, component)
			if err := os.Mkdir(current, 0700); err != nil {
				if !errors.Is(err, os.ErrExist) {
					return err
				}
			} else {
				created = true
			}
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return errors.New("private directory required")
		}
		if created {
			dir, err := os.Open(parent)
			if err != nil {
				return err
			}
			syncErr := dir.Sync()
			closeErr := dir.Close()
			if syncErr != nil {
				return syncErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}

func exclusiveContext(ctx context.Context, path string, b []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Publish a complete file through a hard link, refusing any existing target.
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".backup-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	c := f.Close()
	if e != nil {
		return e
	}
	if c != nil {
		return c
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if e = os.Link(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

func ensureConfigDirectory(path string) error { return os.MkdirAll(path, 0700) }
