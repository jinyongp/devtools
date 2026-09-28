package services

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func appendLogical(history, payload []byte) []byte {
	history = append(history, payload...)
	if len(history) > logLogicalCap {
		history = append([]byte{}, history[len(history)-logLogicalCap:]...)
	}
	return history
}

func TestBoundedLogCompactionStructural(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	stats := &logIOStats{}
	writer := &boundedLog{path: path, stats: stats}

	initial := bytes.Repeat([]byte{'a'}, logLogicalCap)
	if n, err := writer.Write(initial); err != nil || n != len(initial) {
		t.Fatalf("initial write: n=%d err=%v", n, err)
	}
	expected := append([]byte{}, initial...)
	for i := 0; i < 512; i++ {
		chunk := bytes.Repeat([]byte{byte(i)}, 4<<10)
		if n, err := writer.Write(chunk); err != nil || n != len(chunk) {
			t.Fatalf("write %d: n=%d err=%v", i, n, err)
		}
		expected = appendLogical(expected, chunk)
	}

	body, exists, err := (boundedLogStore{path: path}).snapshot(context.Background())
	if err != nil || !exists || !bytes.Equal(body, expected) {
		t.Fatalf("logical snapshot mismatch: len=%d exists=%v err=%v", len(body), exists, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > logPhysicalMax {
		t.Fatalf("physical log exceeded watermark: %d", info.Size())
	}

	stats.mu.Lock()
	appendBytes := stats.AppendBytes
	rewriteBytes := stats.RewriteBytes
	compactions := stats.Compactions
	stats.mu.Unlock()
	if compactions < 6 || compactions > 9 {
		t.Fatalf("unexpected compaction count: %d", compactions)
	}
	if appendBytes == 0 {
		t.Fatal("small writes never used append path")
	}
	if rewriteBytes >= int64(512*logLogicalCap) {
		t.Fatalf("rewrite amplification remained per-write: %d", rewriteBytes)
	}
}

func TestBoundedLogLargeWriteReturnsOriginalLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	writer := &boundedLog{path: path}
	payload := append(bytes.Repeat([]byte{'x'}, 2<<20), []byte("tail")...)
	n, err := writer.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("large write reported short write: n=%d want=%d err=%v", n, len(payload), err)
	}
	body, exists, err := (boundedLogStore{path: path}).snapshot(context.Background())
	if err != nil || !exists || len(body) != logLogicalCap || !bytes.HasSuffix(body, []byte("tail")) {
		t.Fatalf("large write logical tail: len=%d exists=%v err=%v", len(body), exists, err)
	}
}

func TestBoundedLogRejectsOversizePhysicalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	if err := maintenance.Write(path, make([]byte, logPhysicalMax+1)); err != nil {
		t.Fatal(err)
	}
	store := boundedLogStore{path: path}
	if _, _, err := store.snapshot(context.Background()); err == nil {
		t.Fatal("oversize physical log was accepted by reader")
	}
	if _, err := store.write([]byte("more")); err == nil {
		t.Fatal("oversize physical log was silently compacted by writer")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != logPhysicalMax+1 {
		t.Fatalf("corrupt physical log was modified: size=%d err=%v", info.Size(), err)
	}
}

func TestBoundedLogLockHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	store := boundedLogStore{path: path}
	if _, err := store.write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	release, err := acquireLogLock(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, _, err := store.snapshot(ctx); err == nil {
		t.Fatal("shared log read crossed exclusive lock")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("log lock cancellation waited too long: %v", elapsed)
	}
}

func TestRetireAndRestoreLogicalLog(t *testing.T) {
	data := t.TempDir()
	id := tasks.ID()
	store := Store{Data: data}
	if err := tasks.PrivateDir(filepath.Join(data, "processes", id)); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("z"), logLogicalCap)
	if _, err := store.logStore(id).write(payload); err != nil {
		t.Fatal(err)
	}
	seen := false
	exists, apiErr := store.RetireLog(context.Background(), id, func(body []byte) *protocol.Error {
		seen = bytes.Equal(body, payload)
		return nil
	})
	if apiErr != nil || !exists || !seen {
		t.Fatalf("retire: exists=%v seen=%v err=%v", exists, seen, apiErr)
	}
	if _, err := os.Stat(store.path(id, "output.log")); !os.IsNotExist(err) {
		t.Fatalf("retired log still exists: %v", err)
	}
	if apiErr := store.RestoreLog(context.Background(), id, payload); apiErr != nil {
		t.Fatal(apiErr)
	}
	body, exists, apiErr := store.LogSnapshot(context.Background(), id)
	if apiErr != nil || !exists || !bytes.Equal(body, payload) {
		t.Fatalf("restored snapshot: exists=%v err=%v", exists, apiErr)
	}
	if apiErr := store.RestoreLog(context.Background(), id, []byte("different")); apiErr == nil || apiErr.Code != "revision_conflict" {
		t.Fatalf("conflicting restore accepted: %v", apiErr)
	}
}
