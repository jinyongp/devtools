package retainedbody

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReclaimPreservesSharedOwnerAndResumesBoundedOrphans(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	s := Store{Root: root}
	ctx := context.Background()
	owner := filepath.Join(root, "archives", "fixture", "payload")
	if err := s.WriteReference(ctx, owner, "archive_payload", []byte("shared body")); err != nil {
		t.Fatal(err)
	}
	kept, err := s.Put(ctx, "archive_payload", []byte("shared body"))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := s.Put(ctx, "archive_payload", []byte("orphan body"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	for _, r := range []Reference{kept, orphan} {
		if err := os.Chtimes(filepath.Join(s.directory(), r.Digest), old, old); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if result, err := s.ReclaimBatch(ctx, 1, false); err != nil || result.Examined > 1 {
			t.Fatal(result, err)
		}
	}
	if _, err := s.Read(kept); err != nil {
		t.Fatal("shared owner lost", err)
	}
	if _, err := os.Stat(filepath.Join(s.directory(), orphan.Digest)); !os.IsNotExist(err) {
		t.Fatal("orphan not reclaimed", err)
	}
	body, err := s.ReadReference(owner)
	if err != nil || string(body) != "shared body" {
		t.Fatal(string(body), err)
	}
}

func TestReclaimIgnoresUnrelatedControlFilesAndKeepsReceiptOnlyBody(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "data")}
	ctx := context.Background()
	owner := filepath.Join(s.Root, "processes", "receipts", "receipt.json")
	if err := s.WriteReference(ctx, owner, "process_record", []byte("frozen replay")); err != nil {
		t.Fatal(err)
	}
	ref, err := s.Put(ctx, "process_record", []byte("frozen replay"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(s.directory(), ref.Digest), old, old); err != nil {
		t.Fatal(err)
	}
	// Malformed non-owner control data cannot trigger a decoder, full-body read
	// or reclamation failure while deciding the lifetime of replay bodies.
	if err := os.WriteFile(filepath.Join(s.Root, "processes", "receipts", "control.txt"), []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := s.ReclaimBatch(ctx, 1, false); err != nil || result.Removed != 0 || result.Examined != 1 {
		t.Fatal(result, err)
	}
	if _, err := s.Read(ref); err != nil {
		t.Fatal("receipt replay lost", err)
	}
}

func TestReclaimCannotPassAnInFlightPublication(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "data")}
	release, err := s.Publication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.Put(context.Background(), "archive_payload", []byte("in-flight publication"))
	if err != nil {
		release()
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	path := filepath.Join(s.directory(), ref.Digest)
	if err := os.Chtimes(path, old, old); err != nil {
		release()
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.ReclaimBatch(ctx, 1, false); done <- err }()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		release()
		t.Fatal("reclaim crossed live publication", err)
	}
	if _, err := os.Stat(path); err != nil {
		release()
		t.Fatal("in-flight body removed", err)
	}
	release()
	owner := filepath.Join(s.Root, "archives", "fixture", "payload")
	if err := s.WriteReference(context.Background(), owner, "archive_payload", []byte("in-flight publication")); err != nil {
		t.Fatal(err)
	}
	if result, err := s.ReclaimBatch(context.Background(), 1, false); err != nil || result.Removed != 0 {
		t.Fatal(result, err)
	}
}
