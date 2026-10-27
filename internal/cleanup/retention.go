package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/valuehandoff"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (e Engine) inventoryHeld(ctx context.Context, profile string, inspection *vault.Inspection, publicKey *vault.Material) retention.Inventory {
	pending := retention.Pending(profile)
	if inspection == nil || ctx.Err() != nil || inspection.CheckHeld() != nil {
		return pending
	}
	out := retention.Inventory{Profile: profile}
	if values.InspectRetentionHeld(ctx, inspection, publicKey, profile, &out) != nil || valuehandoff.InspectRetentionHeld(ctx, inspection, profile, &out) != nil {
		return pending
	}
	for _, dir := range []string{filepath.Join(e.Cache, "task-queries"), filepath.Join(e.Cache, "dashboard", "queries")} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return pending
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return pending
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if !validID(id) || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			expires, err := tasks.QueryCacheExpiry(path)
			if err != nil || expires.IsZero() {
				return pending
			}
			info, err := platformfs.StatPrivate(path)
			if err != nil || !info.Mode().IsRegular() {
				return pending
			}
			state, reason, eligible := "live", "active_query", false
			if !time.Now().Before(expires) {
				state = "expired"
				reason = "recoverable_query_archive"
				eligible = info.Size() <= 128<<20
				if !eligible {
					reason = "archive_byte_limit"
				}
			}
			out.Add("query_cache", state, "root", "public_metadata", reason, path, eligible, info.Size())
		}
	}
	if ctx.Err() != nil || inspection.CheckHeld() != nil {
		return pending
	}
	out.Complete()
	return out
}
