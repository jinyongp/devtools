//go:build !windows

// Package maintenance coordinates storage access with recoverable multi-file replacement.
package maintenance

import (
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"

	"os"
	"path/filepath"
)

func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	i, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !platformfs.DirectoryPrivate(dir, i) {
		return errors.New("private directory required")
	}
	f, e := os.CreateTemp(dir, ".write-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	return syncDir(dir)
}

func syncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

var syncDir = syncDirectory
