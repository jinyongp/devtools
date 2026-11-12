package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/valuehandoff"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (e Engine) inventoryHeld(ctx context.Context, profile string, inspection *vault.Inspection, publicKey *vault.Material) retention.Inventory {
	out := retention.Pending(profile)
	manager := services.Store{Data: e.Data}
	records, condition := manager.StoredMetadata()
	if condition != nil {
		return out
	}
	blocked := manager.RetirementBlocks(ctx, records)
	for _, r := range records {
		if profile != "" && r.Profile != profile {
			continue
		}
		state, reason, eligible := "retained", "within_retention", false
		if r.DetailRetired {
			state, reason = "compacted", "shared_replay_body"
		} else if r.EndedAt == nil {
			reason = "missing_end_time"
		} else if blocked[r.ID] != "" {
			reason = blocked[r.ID]
		} else if time.Since(*r.EndedAt) >= retention.CleanupCandidateAge {
			state, reason, eligible = "expired", "owned_maintenance_pending", true
		}
		out.Add("process_detail", state, "profile", "public_metadata", reason, r.ID, eligible, 0)
		if r.Capture {
			path := filepath.Join(services.Directory(e.Data), r.ID, "output.log")
			if info, err := platformfs.StatPrivate(path); err == nil {
				expired := r.EndedAt != nil && time.Since(*r.EndedAt) >= retention.RawProcessLogAge
				reason := "within_retention"
				if expired {
					reason = "owned_maintenance_pending"
				}
				out.Add("process_log", "stored", "profile", "public_metadata", reason, r.ID, expired && blocked[r.ID] == "", info.Size())
			}
		}
	}
	if exposures, condition := (expose.Store{Data: e.Data}).List(); condition == nil {
		for _, r := range exposures {
			if profile != "" && r.Route.Profile != profile {
				continue
			}
			eligible := r.State == "stopped" && r.EndedAt != nil && time.Since(*r.EndedAt) >= retention.CleanupCandidateAge
			reason := "active_or_unresolved"
			if eligible {
				reason = "owned_maintenance_pending"
			}
			if r.EndedAt == nil {
				reason = "missing_end_time"
			}
			out.Add("exposure_detail", r.State, "profile", "public_metadata", reason, r.ID, eligible, 0)
		}
	}
	for _, dir := range e.ownedCacheDirectories() {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return out
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return out
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if !validID(id) || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			expires, err := tasks.QueryCacheExpiry(path)
			if err != nil || expires.IsZero() {
				return out
			}
			info, err := platformfs.StatPrivate(path)
			if err != nil || !info.Mode().IsRegular() {
				return out
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
	if inspection == nil || ctx.Err() != nil || inspection.CheckHeld() != nil {
		return out
	}
	if values.InspectRetentionHeld(ctx, inspection, publicKey, profile, &out) != nil || valuehandoff.InspectRetentionHeld(ctx, inspection, profile, &out) != nil {
		return out
	}
	out.Complete()
	return out
}
