package retainedbody

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
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/retention"
)

// Reclaim serializes reference publication before collecting owners. Body mtime
// is only an orphan-publication grace period, never an inferred execution end.
func (s Store) Reclaim(ctx context.Context, limit int, dry bool) (int, error) {
	result, err := s.ReclaimBatch(ctx, limit, dry)
	return result.Removed, err
}

type ReclaimResult struct {
	Examined, Removed int
	HasMore           bool
}

func (s Store) ReclaimBatch(ctx context.Context, limit int, dry bool) (ReclaimResult, error) {
	result := ReclaimResult{}
	if limit < 1 {
		return result, nil
	}
	unlock, err := maintenance.LockExclusive(ctx, filepath.Join(s.Root, ".retained-bodies.lock"))
	if err != nil {
		return result, err
	}
	defer unlock()
	refs := map[string]bool{}
	collect := func(path, domain string) error {
		entries, err := os.ReadDir(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			child := filepath.Join(path, entry.Name())
			switch domain {
			case "processes":
				// Only execution records own bodies. Control, launch, attempts,
				// leases and network journals cannot be body references.
				if len(entry.Name()) != 36 {
					continue
				}
				if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
					return errors.New("unsafe retained-body owner")
				}
				child = filepath.Join(child, "record.json")
			case "archives":
				if entry.Type()&os.ModeSymlink != 0 {
					return errors.New("unsafe retained-body owner")
				}
				if !entry.IsDir() {
					continue
				}
				child = filepath.Join(child, "payload")
			default:
				if !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
			}
			raw, err := maintenance.Read(child, MaxBytes)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			var envelope struct {
				Version int        `json:"storage_version"`
				Body    *Reference `json:"body"`
			}
			if json.Unmarshal(raw, &envelope) != nil || envelope.Version != 0 && envelope.Version != 2 {
				return errors.New("unverified retained-body owner")
			}
			if envelope.Body != nil && envelope.Version != 2 || envelope.Version == 2 && envelope.Body == nil && domain != "receipts" {
				return errors.New("invalid retained-body owner")
			}
			if envelope.Version == 2 && envelope.Body != nil {
				if !envelope.Body.Valid() {
					return errors.New("invalid retained-body owner")
				}
				refs[envelope.Body.Digest] = true
			}
		}
		return nil
	}
	for _, domain := range []string{"processes", "archives", "exposures"} {
		if err := collect(filepath.Join(s.Root, domain), domain); err != nil {
			return result, err
		}
	}
	if err := collect(filepath.Join(s.Root, "processes", "receipts"), "receipts"); err != nil {
		return result, err
	}
	entries, err := os.ReadDir(s.directory())
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	positionPath := filepath.Join(s.Root, ".retention", "body-position.json")
	var position struct {
		After string `json:"after"`
	}
	if raw, err := maintenance.Read(positionPath, 1<<20); err == nil {
		if json.Unmarshal(raw, &position) != nil {
			return result, errors.New("invalid body maintenance position")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	start := sort.Search(len(entries), func(i int) bool { return entries[i].Name() > position.After })
	if start == len(entries) {
		start = 0
	}
	end := min(start+limit, len(entries))
	for _, entry := range entries[start:end] {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.Examined++
		if len(entry.Name()) != 64 || refs[entry.Name()] {
			continue
		}
		path := filepath.Join(s.directory(), entry.Name())
		info, err := platformfs.StatPrivate(path)
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() {
			return result, errors.New("invalid retained body")
		}
		if time.Since(info.ModTime()) < retention.CleanupCandidateAge {
			continue
		}
		if !dry {
			if err := os.Remove(path); err != nil {
				return result, err
			}
		}
		result.Removed++
	}
	result.HasMore = end < len(entries)
	if !dry {
		position.After = ""
		if end < len(entries) && end > start {
			position.After = entries[end-1].Name()
		}
		raw, _ := json.Marshal(position)
		if err := maintenance.Write(positionPath, raw); err != nil {
			return result, err
		}
	}
	return result, nil
}
