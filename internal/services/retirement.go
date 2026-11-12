package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
)

// RetirementBlocks is evaluated once under the process operation barrier.
// A different live server in the same instance is not ownership of this record.
func (s Store) RetirementBlocks(ctx context.Context, records []Record) map[string]string {
	blocked := map[string]string{}
	byID := map[string]Record{}
	latest := map[string]Record{}
	for _, r := range records {
		byID[r.ID] = r
		key := r.Profile + "\x00" + r.Directory + "\x00" + r.Command
		if previous, ok := latest[key]; !ok || previous.CreatedAt.Before(r.CreatedAt) {
			latest[key] = r
		}
	}
	var protect func(string)
	protect = func(id string) {
		if id == "" || blocked[id] == "restart_chain" {
			return
		}
		blocked[id] = "restart_chain"
		if parent, ok := byID[id]; ok {
			protect(parent.Previous)
			protect(parent.AutoOrigin)
		}
	}
	for _, r := range records {
		if r.EndedAt == nil {
			blocked[r.ID] = "missing_end_time"
			protect(r.Previous)
			protect(r.AutoOrigin)
		} else if !s.leaseFree(ctx, r.ID) {
			blocked[r.ID] = "lease_active_or_unknown"
		} else if last := latest[r.Profile+"\x00"+r.Directory+"\x00"+r.Command]; last.ID == r.ID && (r.State == "failed" || r.State == "interrupted" || r.ExitCode != nil && *r.ExitCode != 0) {
			blocked[r.ID] = "latest_unresolved_result"
		}
	}
	// Pending batches may have published a child before their response receipt.
	// Preserve all identities mentioned by unresolved lifecycle/network work.
	for _, directory := range []string{filepath.Join(maintenance.RuntimeDirectory(s.Data, "project-operations"), "receipts"), maintenance.RuntimeDirectory(s.Data, "network-operations")} {
		entries, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			for id := range byID {
				blocked[id] = "reference_verification_pending"
			}
			continue
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			raw, err := maintenance.Read(filepath.Join(directory, entry.Name()), 128<<20)
			var value any
			if err != nil || json.Unmarshal(raw, &value) != nil {
				for id := range byID {
					blocked[id] = "reference_verification_pending"
				}
				continue
			}
			var visit func(any)
			visit = func(v any) {
				switch v := v.(type) {
				case map[string]any:
					if done, ok := v["done"].(bool); ok && done {
						return
					}
					for _, child := range v {
						visit(child)
					}
				case []any:
					for _, child := range v {
						visit(child)
					}
				case string:
					if _, ok := byID[v]; ok {
						protect(v)
					}
				}
			}
			visit(value)
		}
	}
	return blocked
}
