package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Model cancellation immediately after a successful preflight check. The
// context is canceled before the next check, without timing a filesystem write.
type cancelAfterCheck struct {
	context.Context
	cancel context.CancelFunc
}

func (c cancelAfterCheck) Err() error {
	err := c.Context.Err()
	c.cancel()
	return err
}

func TestExclusiveContextCancellationBeforePublication(t *testing.T) {
	for _, afterPreflight := range []bool{false, true} {
		name := "before_preflight"
		if afterPreflight {
			name = "after_preflight"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "transfer.age")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requestContext context.Context = ctx
			if afterPreflight {
				requestContext = cancelAfterCheck{Context: ctx, cancel: cancel}
			} else {
				cancel()
			}
			if err := exclusiveContext(requestContext, path, []byte("encrypted fixture")); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation, got %v", err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("canceled operation published an archive: %v", err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatalf("canceled operation left a partial file: entries=%d err=%v", len(entries), err)
			}
		})
	}
}
