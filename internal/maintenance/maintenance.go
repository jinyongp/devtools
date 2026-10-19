// Package maintenance coordinates storage access with recoverable multi-file replacement.
package maintenance

import (
	"context"

	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Root supports domain stores as well as standalone stores used by callers.
func Root(directory string) string {
	if b := filepath.Base(directory); b == "profiles" || b == "tasks" {
		return filepath.Dir(directory)
	}
	return directory
}

func Acquire(ctx context.Context, root string) (func(), error) {
	return AcquireExclusive(ctx, root)
}

// Read accepts only private regular files and bounds allocation.
func Read(path string, limit int64) ([]byte, error) {
	f, e := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if platformfs.CheckFile(f.File) != nil || i.Size() > limit {
		return nil, errors.New("invalid private file")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("file too large")
	}
	return b, e
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

func recoverPending(root string) error {
	pointer, legacy, body, err := readPointer(root)
	if err != nil {
		return err
	}
	if body == nil {
		return cleanupOrphanTransactions(root, "")
	}
	if !legacy && pointer.Version == 3 && pointer.Phase == "ready" {
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
	pointer.Phase = "committed"
	_ = postCommitCleanup(root, pointer)
	return nil
}
