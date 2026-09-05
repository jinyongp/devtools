package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecoveryBeforeNextReader(t *testing.T) {
	root := t.TempDir()
	release, e := Acquire(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "profiles", "61.json")
	if e = Write(path, []byte("before")); e != nil {
		t.Fatal(e)
	}
	entries := []before{{Path: "profiles/61.json", Data: []byte("before"), Exists: true}, {Path: "tasks/61.json", Exists: false}}
	b, _ := json.Marshal(entries)
	if e = Write(filepath.Join(root, ".maintenance", "restore-pending.json"), b); e != nil {
		t.Fatal(e)
	}
	if e = Write(path, []byte("after")); e != nil {
		t.Fatal(e)
	}
	if e = Write(filepath.Join(root, "tasks", "61.json"), []byte("partial")); e != nil {
		t.Fatal(e)
	}
	release()
	release, e = Acquire(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	b, e = Read(path, 1024)
	if e != nil || string(b) != "before" {
		t.Fatalf("recovery: %q %v", b, e)
	}
	if _, e = os.Stat(filepath.Join(root, "tasks", "61.json")); !os.IsNotExist(e) {
		t.Fatal("partial new file retained")
	}
}
func TestLockCancellationAndReplace(t *testing.T) {
	root := t.TempDir()
	release, e := Acquire(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, e = Acquire(ctx, root); e == nil {
		t.Fatal("lock did not serialize")
	}
	if e = Replace(root, map[string][]byte{"profiles/61.json": []byte("new"), "tasks/61.json": []byte("history")}); e != nil {
		t.Fatal(e)
	}
	release()
	release, e = Acquire(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	b, e := Read(filepath.Join(root, "profiles", "61.json"), 1024)
	if e != nil || string(b) != "new" {
		t.Fatal("committed data rolled back")
	}
	if e = Replace(root, map[string][]byte{"../outside": []byte("bad")}); e == nil {
		t.Fatal("accepted traversal")
	}
}
