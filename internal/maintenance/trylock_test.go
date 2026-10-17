package maintenance

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/testfixture"
	"path/filepath"
	"testing"
)

func TestTryLockExclusiveDistinguishesFreeBusyAndCanceled(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	checked, err := platformfs.OpenRoot(root, true)
	if err != nil {
		t.Fatal(err)
	}
	checked.Close()
	path := filepath.Join(root, "lease.lock")
	release, acquired, err := TryLockExclusive(context.Background(), path)
	if err != nil || !acquired {
		t.Fatal(acquired, err)
	}
	_, busy, err := TryLockExclusive(context.Background(), path)
	if err != nil || busy {
		t.Fatal("busy lease", busy, err)
	}
	release()
	release, acquired, err = TryLockExclusive(context.Background(), path)
	if err != nil || !acquired {
		t.Fatal("released lease", acquired, err)
	}
	release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, acquired, err := TryLockExclusive(ctx, path); acquired || !errors.Is(err, context.Canceled) {
		t.Fatal(acquired, err)
	}
	// Holding only writer intent must also prevent acquisition of a free gate.
	turn, err := acquireFileLock(context.Background(), path+".turnstile", lockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFile(turn)
	if _, acquired, err := TryLockExclusive(context.Background(), path); acquired || err != nil {
		t.Fatal("writer intent", acquired, err)
	}
}

func TestTryLockExclusiveRejectsUnsafeDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	if err := testfixture.MakePublic(path); err != nil {
		t.Fatal(err)
	}
	if release, acquired, err := TryLockExclusive(context.Background(), filepath.Join(path, "lease.lock")); err == nil || acquired || release != nil {
		t.Fatal("unsafe lock accepted", acquired, err)
	}
}
