// Package maintenance coordinates storage access with recoverable multi-file replacement.
package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Root supports domain stores as well as standalone stores used by callers.
func Root(directory string) string {
	if b := filepath.Base(directory); b == "profiles" || b == "tasks" {
		return filepath.Dir(directory)
	}
	return directory
}

func Acquire(ctx context.Context, root string) (func(), error) {
	lockDir := filepath.Join(root, ".maintenance")
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(lockDir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private storage required")
	}
	f, err := os.OpenFile(filepath.Join(lockDir, "lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, errors.New("private lock required")
	}
	for {
		if ctx.Err() != nil {
			f.Close()
			return nil, ctx.Err()
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	release := func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }
	if err = recoverPending(root); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// Read accepts only private regular files and bounds allocation.
func Read(path string, limit int64) ([]byte, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 || i.Size() > limit {
		return nil, errors.New("invalid private file")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("file too large")
	}
	return b, e
}

func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	i, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !i.IsDir() || i.Mode().Perm()&0077 != 0 {
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
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

type before struct {
	Path   string `json:"path"`
	Data   []byte `json:"data"`
	Exists bool   `json:"exists"`
}

func validPath(p string) bool {
	parts := strings.Split(p, "/")
	return len(parts) == 2 && (parts[0] == "profiles" || parts[0] == "tasks" || parts[0] == "backup-receipts") && strings.HasSuffix(parts[1], ".json") && parts[1] != ".json" && !strings.Contains(parts[1], "..") && !strings.Contains(parts[1], "\\")
}

func recoverLegacy(root string, body []byte) error {
	var entries []before
	if json.Unmarshal(body, &entries) != nil {
		return errors.New("invalid recovery journal")
	}
	for _, entry := range entries {
		if !validPath(entry.Path) {
			return errors.New("invalid recovery path")
		}
	}
	for _, entry := range entries {
		target, err := targetPath(root, entry.Path, true)
		if err != nil {
			return err
		}
		if entry.Exists {
			if err := Write(target, entry.Data); err != nil {
				return err
			}
			continue
		}
		if err := removeTarget(target); err != nil {
			return err
		}
	}
	path := pendingPath(root)
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func recoverPending(root string) error {
	pointer, legacy, body, err := readPointer(root)
	if err != nil {
		return err
	}
	if body == nil {
		return cleanupOrphanTransactions(root, "")
	}
	if legacy {
		if err := recoverLegacy(root, body); err != nil {
			return err
		}
		return cleanupOrphanTransactions(root, "")
	}
	if err := recoverTransaction(root, pointer); err != nil {
		return err
	}
	return cleanupOrphanTransactions(root, "")
}

var postCommitCleanup = cleanupTransaction

// Replace requires Acquire's lock. A durable on-disk before-image restores all
// files after an error or process interruption, before another cooperating
// reader proceeds.
func Replace(root string, files map[string][]byte) error {
	if len(files) == 0 {
		return nil
	}
	pointer, err := prepareTransaction(root, files)
	if err != nil {
		return err
	}
	if err := applyReplacement(root, files); err != nil {
		if rollbackErr := recoverTransaction(root, pointer); rollbackErr != nil {
			return errors.New("restore recovery pending")
		}
		return err
	}
	committed, err := markCommitted(root, pointer)
	if err != nil {
		return err
	}
	if !committed {
		return errors.New("restore recovery pending")
	}
	// The durable committed pointer is the logical success boundary. Scratch
	// cleanup is best-effort here; a later Acquire will finish it before allowing
	// another operation to proceed.
	_ = postCommitCleanup(root, transactionPointer{Version: transactionVersion, TransactionID: pointer.TransactionID, Phase: "committed"})
	return nil
}
