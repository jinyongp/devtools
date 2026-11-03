package valuehandoff

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/vault"
)

type RetentionResult struct {
	Examined, Changed int
	Pending           bool
	HasMore           bool
}

// Maintain borrows existing authority before taking any gate. Only bounded
// authenticated metadata is opened; input payloads are never decrypted here.
func Maintain(ctx context.Context, root, profile string, limit int, dry bool) (RetentionResult, error) {
	out := RetentionResult{}
	if limit < 1 {
		return out, nil
	}
	s := Store{Root: root, Profile: "runners"}
	a, err := s.access(ctx, false)
	if err != nil {
		out.Pending = true
		return out, nil
	}
	defer a.master.Destroy()
	scheduled, err := maintenance.LockExclusive(ctx, filepath.Join(root, ".retention", "delivery-maintenance.lock"))
	if err != nil {
		return out, err
	}
	defer scheduled()
	positionPath := filepath.Join(root, ".retention", "delivery-position.json")
	var position struct {
		After string `json:"after"`
	}
	if raw, err := maintenance.Read(positionPath, 1<<20); err == nil {
		if err := json.Unmarshal(raw, &position); err != nil {
			return out, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	paths := []string{}
	dir := filepath.Join(root, "value-executions")
	profiles, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	for _, p := range profiles {
		if profile != "" && p.Name() != profilekey.Key(profile) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(dir, p.Name()))
		if err != nil {
			return out, err
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") && idRE.MatchString(strings.TrimSuffix(entry.Name(), ".json")) {
				paths = append(paths, p.Name()+"/"+entry.Name())
			}
		}
	}
	sort.Strings(paths)
	start := sort.SearchStrings(paths, position.After)
	for start < len(paths) && paths[start] <= position.After {
		start++
	}
	if start == len(paths) {
		start = 0
	}
	end := min(start+limit, len(paths))
	for _, relative := range paths[start:end] {
		id := strings.TrimSuffix(filepath.Base(relative), ".json")
		release, err := maintenance.AcquireQuery(ctx, root)
		if err != nil {
			return out, err
		}
		b, err := s.openBound(ctx, a, "value-executions/"+relative, id, vault.MaterialExecutionRecord)
		release()
		if err != nil {
			return out, err
		}
		var r record
		err = vault.StrictDecode(b.Data, &r)
		clear(b.Data)
		if err != nil || r.Item.ID != id || profilekey.Key(b.Profile) != filepath.Dir(relative) || r.Ticket.Profile != b.Profile {
			return out, vault.ErrIntegrity
		}
		owner := Store{Root: root, Profile: b.Profile}
		out.Examined++
		resolved, err := owner.maintainResolution(ctx, a, r.Item, dry)
		if err != nil {
			return out, err
		}
		if dry {
			continue
		}
		if err := owner.cleanupReplay(ctx, a, id); err != nil {
			return out, err
		}
		// Failed and unknown work stays actionable until a later operation can
		// prove resolution. Missing historical end times are never synthesized.
		if r.RetiredAt != nil || r.Item.EndedAt == nil || time.Since(*r.Item.EndedAt) < retention.CleanupCandidateAge || r.Item.DeliveryState == "delivering" || r.Item.DeliveryState == "unknown" || r.Item.ExecutionState == "reported_failed" && !resolved || r.Item.ExecutionState == "reported_started" {
			continue
		}
		op, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(root, "value-execution-locks", profilekey.Key(b.Profile), id+".lock"))
		if err != nil {
			return out, err
		}
		if !free {
			continue
		}
		gate, err := owner.lockCommit(ctx, a)
		if err != nil {
			op()
			return out, err
		}
		current, err := owner.readRecord(ctx, a, id)
		if err == nil && current.Item.Revision == r.Item.Revision {
			now := time.Now().UTC()
			current.RetiredAt = &now
			current.Slots = nil
			current.Protection = [32]byte{}
			current.Ticket = vault.LaunchTicket{Profile: b.Profile}
			path := owner.path("value-executions", id)
			var body []byte
			body, err = owner.encode(ctx, a, path, id, vault.MaterialExecutionRecord, current)
			if err == nil {
				err = maintenance.Replace(root, map[string][]byte{path: body})
				clear(body)
				if err == nil {
					out.Changed++
				}
			}
		}
		gate()
		op()
		if err != nil {
			return out, err
		}
	}
	out.HasMore = end < len(paths)
	if !dry {
		position.After = ""
		if out.HasMore && end > start {
			position.After = paths[end-1]
		}
		raw, _ := json.Marshal(position)
		if err := maintenance.Write(positionPath, raw); err != nil {
			return out, err
		}
	}
	return out, nil
}
