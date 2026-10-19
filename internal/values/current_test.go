package values

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestCurrentInvalidProfileDoesNotStartStorageOrSession(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	calls := 0
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "../invalid", Master: func(context.Context) (*vault.Material, error) { calls++; return nil, vault.ErrUnavailable }}
	if _, _, e := store.PlanImport(ctx, map[string]string{"TOKEN": "synthetic-input"}, nil, "", false); e == nil || e.Code != "invalid_argument" {
		t.Fatal("invalid import profile not rejected first", e)
	}
	if _, e := CaptureCurrent(ctx, root, "../invalid", "", true, time.Now()); e == nil || e.Code != "invalid_argument" {
		t.Fatal("invalid execution profile not rejected first", e)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 || calls != 0 {
		t.Fatal("invalid selection touched storage/session", err)
	}
}
