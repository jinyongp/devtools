package values

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestVaultContextErrorsPreserveCause(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
		exit  int
	}{
		{context.Canceled, "canceled", 130}, {context.DeadlineExceeded, "deadline_exceeded", 3},
	} {
		t.Run(tc.code, func(t *testing.T) {
			e := VaultError(context.Background(), errors.Join(errors.New("storage wait"), tc.cause))
			if e.Code != tc.code || e.ExitCode != tc.exit {
				t.Fatal(e)
			}
		})
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if e := VaultError(ctx, errors.New("gate wait")); e.Code != "deadline_exceeded" {
		t.Fatal(e)
	}
}

func TestModernCancellationBeforePublication(t *testing.T) {
	root := privateTempDir(t)
	s := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "cancelled"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed, e := s.Update(ctx, func(state *State) (bool, *protocol.Error) {
		result, e := state.Set(Variable, "URL", "", "synthetic-cancelled")
		cancel()
		return result, e
	})
	if changed || e == nil || e.Code != "canceled" {
		t.Fatalf("changed=%v error=%v", changed, e)
	}
	entries, err := os.ReadDir(s.Directory)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			t.Fatal("cancelled candidate was published")
		}
	}
}
