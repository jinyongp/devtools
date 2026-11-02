package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPassiveQueryDoesNotInitialize(t *testing.T) {
	root := filepath.Join(privateTempDir(t), "absent")
	if release, err := AcquirePassiveQuery(context.Background(), root); err == nil {
		release()
		t.Fatal("uninitialized root admitted")
	} else {
		var required *QueryPreparationError
		if !errors.As(err, &required) || required.State != "initialization_required" || !required.Passive {
			t.Fatal("missing passive preparation state", err)
		}
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("passive inspection created root", err)
	}
	release, err := AcquireShared(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	release()
	release, err = AcquirePassiveQuery(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := Write(pendingPath(root), []byte(`[]`)); err != nil {
		t.Fatal(err)
	}
	if release, err := AcquirePassiveQuery(context.Background(), root); err == nil {
		release()
		t.Fatal("pending recovery admitted")
	} else {
		var required *QueryPreparationError
		if !errors.As(err, &required) || required.State != "recovery_required" || !required.Passive {
			t.Fatal("missing passive recovery state", err)
		}
	}
	if _, err := os.Lstat(pendingPath(root)); err != nil {
		t.Fatal("inspection recovered pending transaction", err)
	}
}
