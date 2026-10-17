package platformfs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// External artifacts have private files, but may live in an existing public
// directory selected by the user. Never repair that directory's ACL.
func externalPath(path string) (string, error) {
	if path == "" {
		return "", ErrUnsafe
	}
	if v := filepath.VolumeName(path); v != "" && !filepath.IsAbs(path) {
		return "", ErrUnsafe
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return privatePath(absolute)
}
func OpenExternalPrivate(path string, flags int, _ os.FileMode) (*File, error) {
	absolute, err := externalPath(path)
	if err != nil {
		return nil, err
	}
	guard, err := guardDirectory(filepath.Dir(absolute), "", false)
	if err != nil {
		return nil, err
	}
	f, err := openPrivateFile(absolute, flags)
	if err == nil && flags&os.O_TRUNC != 0 {
		err = f.Truncate(0)
	}
	if err != nil {
		if f != nil {
			_ = f.Close()
		}
		_ = guard.close()
		return nil, err
	}
	return &File{File: f, guard: guard}, nil
}
func EnsureExternalDirectory(path string) error {
	absolute, err := externalPath(path)
	if err != nil {
		return err
	}
	guard, err := guardDirectory(absolute, "", true)
	if err != nil {
		return err
	}
	return guard.close()
}
func WriteExternalPrivate(ctx context.Context, path string, body []byte, replace bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	absolute, err := externalPath(path)
	if err != nil {
		return err
	}
	guard, err := guardDirectory(filepath.Dir(absolute), "", false)
	if err != nil {
		return err
	}
	defer guard.close()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(absolute), ".write-"+hex.EncodeToString(random[:]))
	f, err := openPrivateFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return publish(ctx, temp, absolute, replace)
}
func RemoveExternalPrivate(ctx context.Context, path string) error {
	absolute, err := externalPath(path)
	if err != nil {
		return err
	}
	guard, err := guardDirectory(filepath.Dir(absolute), "", false)
	if err != nil {
		return err
	}
	defer guard.close()
	return removeGuardedFile(ctx, absolute)
}
