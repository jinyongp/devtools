package platformfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/testfs"
)

func testRoot(t *testing.T) *Root {
	t.Helper()
	// Darwin temp paths start at the system /var alias; keep the SDK no-follow
	// contract and pass the existing physical fixture directory.
	base := testfs.TempDir(t)
	root, err := OpenRoot(filepath.Join(base, "private"), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}
func TestPrivateFilePublish(t *testing.T) {
	root := testRoot(t)
	if err := root.MkdirAll("nested"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := root.WriteFile(ctx, "nested/state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(ctx, "nested/state", []byte("forbidden"), false); err == nil {
		t.Fatal("no-replace overwrote an existing target")
	}
	body, err := root.ReadFile("nested/state", 10)
	if err != nil || string(body) != "old" {
		t.Fatalf("old snapshot: %q, %v", body, err)
	}
	if err := root.WriteFile(ctx, "nested/state", []byte("new"), true); err != nil {
		t.Fatal(err)
	}
	body, err = root.ReadFile("nested/state", 10)
	if err != nil || string(body) != "new" {
		t.Fatalf("new snapshot: %q, %v", body, err)
	}
	entries, err := os.ReadDir(filepath.Join(root.path, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state" {
		t.Fatal("temporary publication artifacts leaked")
	}
}
func TestPrivateReadLimitAndMissing(t *testing.T) {
	root := testRoot(t)
	if _, err := root.ReadFile("missing", 100); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	if err := root.WriteFile(context.Background(), "state", []byte("1234"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := root.ReadFile("state", 3); err == nil {
		t.Fatal("read limit ignored")
	}
	if _, err := root.ReadFile("state", -1); err == nil {
		t.Fatal("negative limit accepted")
	}
}
func TestCancelledPublishPreservesTarget(t *testing.T) {
	root := testRoot(t)
	if err := root.WriteFile(context.Background(), "state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.WriteFile(ctx, "state", []byte("new"), true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	body, err := root.ReadFile("state", 10)
	if err != nil || string(body) != "old" {
		t.Fatalf("cancel altered target: %q %v", body, err)
	}
}
func TestRootRejectsEscapeAndUseAfterClose(t *testing.T) {
	root := testRoot(t)
	for _, name := range []string{"../outside", filepath.Join(root.path, "absolute"), ""} {
		if _, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("escape accepted: %q: %v", name, err)
		}
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.ReadFile("file", 10); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed root: %v", err)
	}
}
func TestTruncateOnlyAfterValidation(t *testing.T) {
	root := testRoot(t)
	if err := root.WriteFile(context.Background(), "state", []byte("old"), false); err != nil {
		t.Fatal(err)
	}
	f, err := root.OpenFile("state", os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := root.ReadFile("state", 10)
	if err != nil || len(data) != 0 {
		t.Fatalf("truncate: %q %v", data, err)
	}
}
