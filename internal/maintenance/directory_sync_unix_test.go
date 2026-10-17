//go:build !windows

package maintenance

import (
	"context"

	"errors"

	"fmt"
	"os"
	"path/filepath"

	"testing"
)

func TestCommitPointerSyncFailurePreservesRecoveryState(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint("persistent=", persistent), func(t *testing.T) {
			root := privateTempDir(t)
			release, err := Acquire(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if err := Write(filepath.Join(root, "profiles", "61.json"), []byte("before")); err != nil {
				t.Fatal(err)
			}
			pointer, err := prepareTransaction(root, map[string][]byte{"profiles/61.json": []byte("after")})
			if err != nil {
				t.Fatal(err)
			}
			if err := applyReplacement(root, map[string][]byte{"profiles/61.json": []byte("after")}); err != nil {
				t.Fatal(err)
			}
			syncs := 0
			committed, err := markCommittedWithIO(root, pointer, func(path string, body []byte) error {
				if err := Write(path, body); err != nil {
					return err
				}
				// Model the write's post-rename directory sync reporting failure.
				return errors.New("injected commit directory sync failure")
			}, func(path string) error {
				syncs++
				if persistent {
					return errors.New("persistent directory sync failure")
				}
				return syncDir(path)
			})
			if syncs != 1 || committed == persistent || (err != nil) != persistent {
				t.Fatalf("commit durability not established: committed=%v err=%v syncs=%d", committed, err, syncs)
			}
			if _, err := os.Stat(transactionDir(root, pointer.TransactionID)); err != nil {
				t.Fatal("before-images were discarded", err)
			}
			observed, _, _, err := readPointer(root)
			if err != nil || observed.Phase != "committed" {
				t.Fatal(observed, err)
			}
		})
	}
}

func TestRecoveryRetainsBeforeImagesUntilPointerRemovalIsDurable(t *testing.T) {
	root := privateTempDir(t)
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "profiles", "61.json")
	if err := Write(target, []byte("before")); err != nil {
		release()
		t.Fatal(err)
	}
	originalSync := syncDir
	failCheckpoint, sawCommit := true, false
	syncDir = func(path string) error {
		if path == filepath.Dir(pendingPath(root)) && failCheckpoint {
			pointer, _, body, err := readPointer(root)
			if err == nil && body != nil && pointer.Phase == "committed" {
				sawCommit = true
			}
			if sawCommit {
				return errors.New("injected persistent maintenance directory sync failure")
			}
		}
		return originalSync(path)
	}
	defer func() { syncDir = originalSync }()
	if err := Replace(root, map[string][]byte{"profiles/61.json": []byte("after")}); err == nil {
		release()
		t.Fatal("uncertain commit reported success")
	}
	pointer, _, _, err := readPointer(root)
	if err != nil || pointer.Phase != "committed" {
		release()
		t.Fatal(pointer, err)
	}
	release()
	for attempt := 0; attempt < 2; attempt++ {
		unlock, err := Acquire(context.Background(), root)
		if unlock != nil {
			unlock()
		}
		if err == nil {
			t.Fatalf("recovery attempt %d discarded rollback state before a durable pointer checkpoint", attempt)
		}
		if _, err := os.Stat(transactionDir(root, pointer.TransactionID)); err != nil {
			t.Fatal("before-images removed", err)
		}
	}
	failCheckpoint = false
	unlock, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if mustRead(t, target) != "after" {
		t.Fatal("committed values rolled back")
	}
	if _, err := os.Stat(transactionDir(root, pointer.TransactionID)); !os.IsNotExist(err) {
		t.Fatal("durable recovery did not clean before-images", err)
	}
}
