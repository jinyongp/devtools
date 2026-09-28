package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var largeStorageTest = flag.Bool("large-storage-test", false, "run large maintenance replacement test")

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

func mustRead(t *testing.T, path string) string {
	t.Helper()
	body, err := Read(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestV2ApplyingRecoveryRestoresAllTargets(t *testing.T) {
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profiles", "61.json")
	task := filepath.Join(root, "tasks", "61.json")
	if err := Write(profile, []byte("profile-before")); err != nil {
		t.Fatal(err)
	}
	if err := Write(task, []byte("task-before")); err != nil {
		t.Fatal(err)
	}
	pointer, err := prepareTransaction(root, map[string][]byte{
		"profiles/61.json": []byte("profile-after"),
		"tasks/61.json":    []byte("task-after"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if pointer.Phase != "applying" {
		t.Fatalf("unexpected pointer: %#v", pointer)
	}
	if err := Write(profile, []byte("profile-after")); err != nil {
		t.Fatal(err)
	}
	if err := Write(task, []byte("task-after")); err != nil {
		t.Fatal(err)
	}
	release()

	release, err = Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if got := mustRead(t, profile); got != "profile-before" {
		t.Fatalf("profile rollback = %q", got)
	}
	if got := mustRead(t, task); got != "task-before" {
		t.Fatalf("task rollback = %q", got)
	}
	if _, err := os.Stat(pendingPath(root)); !os.IsNotExist(err) {
		t.Fatalf("pending pointer survived recovery: %v", err)
	}
}

func TestV2RecoveryCanRepeatAfterPartialRollback(t *testing.T) {
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profiles", "61.json")
	task := filepath.Join(root, "tasks", "61.json")
	if err := Write(profile, []byte("profile-before")); err != nil {
		t.Fatal(err)
	}
	if err := Write(task, []byte("task-before")); err != nil {
		t.Fatal(err)
	}
	pointer, err := prepareTransaction(root, map[string][]byte{
		"profiles/61.json": []byte("profile-after"),
		"tasks/61.json":    []byte("task-after"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(profile, []byte("profile-after")); err != nil {
		t.Fatal(err)
	}
	if err := Write(task, []byte("task-after")); err != nil {
		t.Fatal(err)
	}
	manifest, err := readManifest(root, pointer)
	if err != nil {
		t.Fatal(err)
	}
	first := manifest.Entries[0]
	if !first.Exists {
		t.Fatal("expected before image")
	}
	firstTarget, err := targetPath(root, first.Path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := restorePrivateFile(filepath.Join(transactionDir(root, pointer.TransactionID), "before", first.BeforePath), firstTarget); err != nil {
		t.Fatal(err)
	}
	// Simulate a second crash after only one rollback target was published.
	release()

	release, err = Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if got := mustRead(t, profile); got != "profile-before" {
		t.Fatalf("profile rollback = %q", got)
	}
	if got := mustRead(t, task); got != "task-before" {
		t.Fatalf("task rollback = %q", got)
	}
}

func TestCommittedRecoveryNeverRollsBack(t *testing.T) {
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "profiles", "61.json")
	if err := Write(target, []byte("before")); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"profiles/61.json": []byte("after")}
	pointer, err := prepareTransaction(root, files)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyReplacement(root, files); err != nil {
		t.Fatal(err)
	}
	manifest, err := readManifest(root, pointer)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 || manifest.Entries[0].BeforePath == "" {
		t.Fatalf("missing committed before image: %#v", manifest.Entries)
	}
	// Once the commit pointer is durable, before-images are cleanup-only scratch.
	// Corrupt scratch must not turn a committed mutation back into a rollback.
	if err := Write(filepath.Join(transactionDir(root, pointer.TransactionID), "before", manifest.Entries[0].BeforePath), []byte("corrupt-after-commit")); err != nil {
		t.Fatal(err)
	}
	pointer.Phase = "committed"
	body, _ := json.Marshal(pointer)
	if err := Write(pendingPath(root), body); err != nil {
		t.Fatal(err)
	}
	release()

	release, err = Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if got := mustRead(t, target); got != "after" {
		t.Fatalf("committed data rolled back: %q", got)
	}
}

func TestAcquireRemovesUnreferencedTransactionScratch(t *testing.T) {
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	id, err := transactionID()
	if err != nil {
		t.Fatal(err)
	}
	dir, _, err := createTransactionDirectories(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(dir, "manifest.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	release()

	release, err = Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("orphan transaction survived Acquire: %v", err)
	}
}

func TestReplaceRejectsNestedIdentitySymlink(t *testing.T) {
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	profiles := filepath.Join(root, "profiles")
	if err := os.Mkdir(profiles, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(profiles, ".identity")); err != nil {
		t.Fatal(err)
	}
	key := "p1-" + strings.Repeat("a", 64)
	if err := Replace(root, map[string][]byte{"profiles/.identity/" + key + ".json": []byte("{}")}); err == nil {
		t.Fatal("nested identity symlink accepted")
	}
}

func TestReplaceLargeBeforeImage(t *testing.T) {
	if !*largeStorageTest && os.Getenv("DEVTOOLS_LARGE_STORAGE_TEST") != "1" {
		t.Skip("use -large-storage-test for >128 MiB streaming replacement")
	}
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	target, err := targetPath(root, "tasks/61.json", true)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(129 << 20); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syncDir(filepath.Dir(target)); err != nil {
		t.Fatal(err)
	}
	if err := Replace(root, map[string][]byte{"tasks/61.json": []byte("migrated")}); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, target); got != "migrated" {
		t.Fatalf("large replacement = %q", got)
	}
}

func TestApplyingRecoveryValidatesAllBeforeImagesBeforeMutation(t *testing.T) {
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profiles", "61.json")
	task := filepath.Join(root, "tasks", "61.json")
	if err := Write(profile, []byte("profile-before")); err != nil {
		t.Fatal(err)
	}
	if err := Write(task, []byte("task-before")); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"profiles/61.json": []byte("profile-after"),
		"tasks/61.json":    []byte("task-after"),
	}
	pointer, err := prepareTransaction(root, files)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyReplacement(root, files); err != nil {
		t.Fatal(err)
	}
	manifest, err := readManifest(root, pointer)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 2 {
		t.Fatalf("manifest entries = %d", len(manifest.Entries))
	}
	corrupt := manifest.Entries[1]
	if !corrupt.Exists {
		t.Fatal("expected before image")
	}
	beforePath := filepath.Join(transactionDir(root, pointer.TransactionID), "before", corrupt.BeforePath)
	if err := Write(beforePath, []byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	release()

	if release, err = Acquire(context.Background(), root); err == nil {
		release()
		t.Fatal("corrupt before image was accepted")
	}
	// No rollback target may be touched until every before-image validates.
	if got := mustRead(t, profile); got != "profile-after" {
		t.Fatalf("profile mutated before validation completed: %q", got)
	}
	if got := mustRead(t, task); got != "task-after" {
		t.Fatalf("task mutated before validation completed: %q", got)
	}
}

func TestLegacyDecoderFailsClosedOnV2Pointer(t *testing.T) {
	body, err := json.Marshal(transactionPointer{
		Version:       transactionVersion,
		TransactionID: "00000000-0000-4000-8000-000000000001",
		Phase:         "applying",
	})
	if err != nil {
		t.Fatal(err)
	}
	var legacy []before
	if json.Unmarshal(body, &legacy) == nil {
		t.Fatal("legacy inline journal decoder accepted v2 pointer")
	}
}

func TestReplacePostCommitCleanupFailureStaysCommitted(t *testing.T) {
	root := t.TempDir()
	release, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "profiles", "61.json")
	if err := Write(target, []byte("before")); err != nil {
		t.Fatal(err)
	}

	originalCleanup := postCommitCleanup
	postCommitCleanup = func(string, transactionPointer) error {
		return errors.New("injected post-commit cleanup failure")
	}
	defer func() { postCommitCleanup = originalCleanup }()

	if err := Replace(root, map[string][]byte{"profiles/61.json": []byte("after")}); err != nil {
		t.Fatalf("committed replacement reported cleanup failure: %v", err)
	}
	if got := mustRead(t, target); got != "after" {
		t.Fatalf("committed target = %q", got)
	}
	pointer, legacy, body, err := readPointer(root)
	if err != nil || legacy || body == nil || pointer.Phase != "committed" {
		t.Fatalf("committed recovery pointer missing: %#v legacy=%v err=%v", pointer, legacy, err)
	}
	postCommitCleanup = originalCleanup
	release()

	release, err = Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if got := mustRead(t, target); got != "after" {
		t.Fatalf("next Acquire rolled back committed target: %q", got)
	}
	if _, err := os.Stat(pendingPath(root)); !os.IsNotExist(err) {
		t.Fatalf("committed pointer survived recovery cleanup: %v", err)
	}
}
