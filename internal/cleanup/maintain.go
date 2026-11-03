package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retainedbody"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/valuehandoff"
)

type MaintenanceItem struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	Eligible bool   `json:"eligible"`
	Changed  bool   `json:"changed"`
}
type MaintenanceResult struct {
	ReclaimedBodies     int               `json:"reclaimed_bodies"`
	VerificationPending bool              `json:"verification_pending"`
	Items               []MaintenanceItem `json:"items"`
	Examined            int               `json:"examined"`
	Changed             int               `json:"changed"`
	HasMore             bool              `json:"has_more"`
	DryRun              bool              `json:"dry_run"`
}
type maintenancePosition struct {
	Version int    `json:"version"`
	After   string `json:"after"`
	Phase   string `json:"phase,omitempty"`
}

// Maintain is a bounded, restartable owner pass. It never touches user-authored
// tasks, manual archives, request identities, or an unresolved external effect.
// Dry runs use the same eligibility checks and leave the saved position intact.
func (e Engine) Maintain(ctx context.Context, profile string, limit int, dry bool) (MaintenanceResult, *protocol.Error) {
	if limit < 1 || limit > 200 {
		return MaintenanceResult{}, fail("invalid_argument")
	}
	scheduled, err := maintenance.LockExclusive(ctx, filepath.Join(e.Data, ".retention", "schedule.lock"))
	if err != nil {
		return MaintenanceResult{}, fail("storage_error", err)
	}
	defer scheduled()
	var turn struct {
		Protected     bool `json:"protected"`
		PublicPending bool `json:"public_pending"`
	}
	turnPath := filepath.Join(e.Data, ".retention", "single-turn.json")
	if limit == 1 {
		if err := tasks.ReadPrivate(turnPath, &turn); err != nil && !errors.Is(err, os.ErrNotExist) {
			return MaintenanceResult{}, fail("storage_error", err)
		}
	}
	publicLimit := (limit + 1) / 2
	if limit == 1 && turn.Protected {
		publicLimit = 0
	}
	out := MaintenanceResult{Items: []MaintenanceItem{}, DryRun: dry}
	if publicLimit > 0 {
		var condition *protocol.Error
		out, condition = e.maintainPublic(ctx, profile, publicLimit, dry)
		if condition != nil {
			return out, condition
		}
	}
	protected, err := valuehandoff.Maintain(ctx, e.Data, profile, limit-publicLimit, dry)
	out.Examined += protected.Examined
	out.Changed += protected.Changed
	out.HasMore = out.HasMore || protected.HasMore
	out.VerificationPending = protected.Pending
	if err != nil {
		return out, fail("storage_error", err)
	}
	if limit == 1 {
		if !turn.Protected {
			turn.PublicPending, out.HasMore = out.HasMore, true
		} else {
			out.HasMore = out.HasMore || turn.PublicPending
		}
		turn.Protected = !turn.Protected
		if !dry {
			if err := write(turnPath, turn); err != nil {
				return out, fail("storage_error", err)
			}
		}
	}
	return out, nil
}

func (e Engine) maintainPublic(ctx context.Context, profile string, limit int, dry bool) (MaintenanceResult, *protocol.Error) {
	out := MaintenanceResult{Items: []MaintenanceItem{}, DryRun: dry}
	if limit < 1 || limit > 200 {
		return out, fail("invalid_argument")
	}
	unlock, condition := e.lock(ctx)
	if condition != nil {
		return out, condition
	}
	defer unlock()
	positionPath := filepath.Join(e.Data, ".retention", "position.json")
	var position maintenancePosition
	if err := tasks.ReadPrivate(positionPath, &position); err != nil && !errors.Is(err, os.ErrNotExist) {
		return out, fail("storage_error", err)
	}
	if position.Version != 0 && position.Version != 1 {
		return out, fail("storage_error")
	}
	if position.Phase != "" && position.Phase != "bodies" {
		return out, fail("storage_error")
	}
	if position.Phase == "bodies" {
		return e.maintainBodies(ctx, out, positionPath, limit, dry)
	}
	manager := services.Store{Data: e.Data}
	records, condition := manager.StoredMetadata()
	if condition != nil {
		return out, condition
	}
	blocked := manager.RetirementBlocks(ctx, records)
	attempts, condition := manager.StoredAttempts()
	if condition != nil {
		return out, condition
	}
	latest := map[string]services.Record{}
	latestStarted := map[string]time.Time{}
	for _, r := range records {
		key := r.Profile + "\x00" + r.Directory + "\x00" + r.Command
		latestStarted[key] = maxTime(latestStarted[key], r.CreatedAt)
		if previous, ok := latest[key]; !ok || previous.CreatedAt.Before(r.CreatedAt) {
			latest[key] = r
		}
	}
	for _, a := range attempts {
		key := a.Profile + "\x00" + a.Directory + "\x00" + a.Command
		latestStarted[key] = maxTime(latestStarted[key], a.CreatedAt)
	}
	type owned struct {
		key      string
		record   *services.Record
		attempt  *services.Attempt
		exposure *expose.Record
		cache    string
	}
	ownedItems := []owned{}
	for _, a := range attempts {
		if profile == "" || profile == a.Profile {
			copy := a
			ownedItems = append(ownedItems, owned{key: "attempt:" + a.ID, attempt: &copy})
		}
	}
	for _, r := range records {
		if profile == "" || profile == r.Profile {
			copy := r
			ownedItems = append(ownedItems, owned{key: "process:" + r.ID, record: &copy})
		}
	}
	exposures, condition := (expose.Store{Data: e.Data}).List()
	if condition != nil {
		return out, condition
	}
	for _, r := range exposures {
		if profile == "" || profile == r.Route.Profile {
			copy := r
			ownedItems = append(ownedItems, owned{key: "exposure:" + r.ID, exposure: &copy})
		}
	}
	for _, dir := range e.ownedCacheDirectories() {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return out, fail("storage_error", err)
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") && validID(strings.TrimSuffix(entry.Name(), ".json")) {
				ownedItems = append(ownedItems, owned{key: "cache:" + dir + ":" + entry.Name(), cache: filepath.Join(dir, entry.Name())})
			}
		}
	}
	sort.Slice(ownedItems, func(i, j int) bool { return ownedItems[i].key < ownedItems[j].key })
	start := sort.Search(len(ownedItems), func(i int) bool { return ownedItems[i].key > position.After })
	if start == len(ownedItems) {
		start = 0
	}
	end := min(start+limit, len(ownedItems))
	now := time.Now().UTC()
	for _, owned := range ownedItems[start:end] {
		if ctx.Err() != nil {
			return out, protocol.ContextError(ctx.Err())
		}
		item := MaintenanceItem{ID: owned.key, Reason: "active_or_unresolved"}
		switch {
		case owned.attempt != nil:
			a := *owned.attempt
			item.Kind = "process_attempt"
			last := latestStarted[a.Profile+"\x00"+a.Directory+"\x00"+a.Command]
			unresolved := !a.CreatedAt.Before(last) && (a.State == "failed" || a.State == "unknown" || a.State == "interrupted")
			if a.RetiredAt != nil {
				item.Reason = "request_summary_retained"
			} else if a.EndedAt == nil {
				item.Reason = "missing_end_time"
			} else if unresolved {
				item.Reason = "latest_unresolved_result"
			} else if now.Sub(*a.EndedAt) >= retention.CleanupCandidateAge {
				item.Eligible, item.Reason = true, "detail_expired_request_preserved"
				if !dry {
					if err := manager.RetireAttempt(ctx, a.ID, now); err != nil {
						return out, fail("storage_error", err)
					}
					item.Changed = true
				}
			}
		case owned.record != nil:
			r := *owned.record
			item.Kind = "process_detail"
			switch {
			case blocked[r.ID] != "":
				item.Reason = blocked[r.ID]
			case r.DetailRetired:
				item.Reason = "shared_replay_body"
			case r.EndedAt == nil:
				item.Reason = "missing_end_time"
			default:
				// Logs expire independently of the detail retention window.
				if now.Sub(*r.EndedAt) >= retention.RawProcessLogAge && r.Capture {
					if _, err := os.Lstat(filepath.Join(e.Data, "processes", r.ID, "output.log")); err == nil {
						item.Kind, item.Eligible, item.Reason = "process_log", true, "log_expired"
						if !dry {
							_, pe := manager.RetireLog(ctx, r.ID, func([]byte) *protocol.Error { return nil })
							if pe != nil {
								return out, pe
							}
							item.Changed = true
						}
					}
				}
				last := latest[r.Profile+"\x00"+r.Directory+"\x00"+r.Command]
				unresolved := last.ID == r.ID && (r.State == "failed" || r.State == "interrupted" || r.ExitCode != nil && *r.ExitCode != 0)
				if !unresolved && now.Sub(*r.EndedAt) >= retention.CleanupCandidateAge {
					item.Kind, item.Eligible, item.Reason = "process_detail", true, "detail_expired_replay_preserved"
					if !dry {
						if err := manager.RetireDetail(r.ID, now); err != nil {
							return out, fail("storage_error", err)
						}
						item.Changed = true
					}
				} else if unresolved {
					item.Reason = "latest_unresolved_result"
				}
			}
		case owned.exposure != nil:
			r := *owned.exposure
			item.Kind = "exposure_detail"
			if r.State == "stopped" && r.EndedAt != nil && now.Sub(*r.EndedAt) >= retention.CleanupCandidateAge {
				item.Eligible, item.Reason = true, "detail_expired_replay_preserved"
				if !dry {
					if pe := (expose.Store{Data: e.Data}).Retire(ctx, r.ID); pe != nil {
						return out, pe
					}
					item.Changed = true
				}
			} else if r.EndedAt == nil {
				item.Reason = "missing_end_time"
			}
		case owned.cache != "":
			item.Kind = "query_cache"
			expires, err := tasks.QueryCacheExpiry(owned.cache)
			if err != nil {
				return out, fail("storage_error", err)
			}
			if !expires.IsZero() && !now.Before(expires) {
				item.Eligible, item.Reason = true, "snapshot_expired"
				if !dry {
					if err := os.Remove(owned.cache); err != nil && !errors.Is(err, os.ErrNotExist) {
						return out, fail("storage_error", err)
					}
					item.Changed = true
				}
			}
		}
		out.Examined++
		if item.Changed {
			out.Changed++
		}
		out.Items = append(out.Items, item)
	}
	out.HasMore = end < len(ownedItems)
	if !out.HasMore {
		return e.maintainBodies(ctx, out, positionPath, limit-out.Examined, dry)
	}
	if !dry {
		after := ""
		if out.HasMore && end > start {
			after = ownedItems[end-1].key
		}
		if err := write(positionPath, maintenancePosition{Version: 1, After: after}); err != nil {
			return out, fail("storage_error", err)
		}
	}
	return out, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

func (e Engine) maintainBodies(ctx context.Context, out MaintenanceResult, positionPath string, remaining int, dry bool) (MaintenanceResult, *protocol.Error) {
	position := maintenancePosition{Version: 1, Phase: "bodies"}
	if remaining == 0 {
		out.HasMore = true
	} else {
		result, err := (retainedbody.Store{Root: e.Data}).ReclaimBatch(ctx, remaining, dry)
		if err != nil {
			return out, fail("storage_error", err)
		}
		out.Examined += result.Examined
		out.ReclaimedBodies = result.Removed
		if !dry {
			out.Changed += result.Removed
		}
		out.HasMore = result.HasMore
		if !result.HasMore {
			position.Phase = ""
		}
	}
	if !dry {
		if err := write(positionPath, position); err != nil {
			return out, fail("storage_error", err)
		}
	}
	return out, nil
}
