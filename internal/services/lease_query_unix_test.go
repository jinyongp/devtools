//go:build linux || darwin

package services

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestQueryLeaseInterlocksWithExistingWriter(t *testing.T) {
	path := filepath.Join(privateTempDir(t), "lease.lock")
	writer, acquired, err := tryLease(path, true)
	if err != nil || !acquired {
		t.Fatal(acquired, err)
	}
	if query, acquired, err := tryQueryLease(path); err != nil || acquired {
		if query != nil {
			query.unlockClose()
		}
		t.Fatal("query crossed writer", acquired, err)
	}
	writer.unlockClose()
	query, acquired, err := tryQueryLease(path)
	if err != nil || !acquired {
		t.Fatal(acquired, err)
	}
	if writer, acquired, err := tryLease(path, false); err != nil || acquired {
		if writer != nil {
			writer.unlockClose()
		}
		t.Fatal("writer crossed query", acquired, err)
	}
	query.unlockClose()
}

func TestQueryLeaseDoesNotCreateAndRejectsUnsafeObjects(t *testing.T) {
	for _, kind := range []string{"missing", "symlink", "fifo", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(privateTempDir(t), "lease.lock")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(path+".target", path)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "permissions":
				err = os.WriteFile(path, nil, 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			lease, acquired, err := tryQueryLease(path)
			if err == nil || acquired {
				if lease != nil {
					lease.unlockClose()
				}
				t.Fatal("unsafe or missing lease admitted", acquired, err)
			}
			if kind == "missing" {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("query created lease", err)
				}
			}
		})
	}
}
