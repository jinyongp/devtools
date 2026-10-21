package valuehandoff

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (s Store) observed(ctx context.Context, r record) (Execution, error) {
	item := r.Item
	if (item.DeliveryState == "prepared" || item.DeliveryState == "approved") && !time.Now().Before(item.ExpiresAt) {
		item.DeliveryState = "expired"
	}
	if item.DeliveryState == "delivering" {
		release, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(s.Root, "value-execution-locks", profilekey.Key(s.Profile), item.ID+".lock"))
		if err != nil {
			return item, err
		}
		if free {
			release()
			item.DeliveryState = "unknown"
		}
	}
	return item, nil
}
func (s Store) Status(ctx context.Context, id, requestID string) (View, *protocol.Error) {
	if (id == "") == (requestID == "") || id != "" && !idRE.MatchString(id) || requestID != "" && !uuidRE.MatchString(requestID) {
		return View{}, invalid()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, err := s.access(ctx, false)
	if err != nil {
		return View{}, values.VaultError(ctx, err)
	}
	defer a.master.Destroy()
	release, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return View{}, values.VaultError(ctx, err)
	}
	if requestID != "" {
		var rr receipt
		err = s.decode(ctx, a, s.path("value-execution-requests", requestID), requestID, vault.MaterialExecutionRequest, &rr)
		if err == nil {
			if rr.Action != "prepare" || !idRE.MatchString(rr.Result.Item.ID) {
				release()
				return View{}, failure("execution_not_found")
			}
			id = rr.Result.Item.ID
		}
	}
	var r record
	if err == nil {
		r, err = s.readRecord(ctx, a, id)
	}
	release()
	if errors.Is(err, os.ErrNotExist) {
		return View{}, failure("execution_not_found")
	}
	if err != nil {
		return View{}, values.VaultError(ctx, err)
	}
	item, err := s.observed(ctx, r)
	if err != nil {
		return View{}, values.VaultError(ctx, err)
	}
	return View{s.Profile, item}, nil
}

type ListResult struct {
	Profile    string      `json:"profile"`
	Items      []Execution `json:"items"`
	NextCursor *string     `json:"next_cursor"`
}
type listCursor struct {
	Revision string `json:"revision"`
	Offset   int    `json:"offset"`
}

func (s Store) List(ctx context.Context, limit int, cursor string) (ListResult, *protocol.Error) {
	out := ListResult{Profile: s.Profile, Items: []Execution{}}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return out, invalid()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, err := s.access(ctx, false)
	if err != nil {
		return out, values.VaultError(ctx, err)
	}
	defer a.master.Destroy()
	release, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return out, values.VaultError(ctx, err)
	}
	dir := filepath.Join(s.Root, "value-executions", profilekey.Key(s.Profile))
	guard, err := platformfs.OpenRoot(dir, false)
	if errors.Is(err, os.ErrNotExist) {
		release()
		if cursor != "" {
			return out, failure("cursor_invalid")
		}
		return out, nil
	}
	if err != nil {
		release()
		return out, values.VaultError(ctx, err)
	}
	defer guard.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		release()
		return out, values.VaultError(ctx, err)
	}
	h := sha256.New()
	h.Write([]byte(s.Root + "\x00" + s.Profile))
	records := []record{}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !strings.HasSuffix(entry.Name(), ".json") || !idRE.MatchString(id) {
			err = vault.ErrIntegrity
			break
		}
		var r record
		r, err = s.readRecord(ctx, a, id)
		if err != nil {
			break
		}
		b, _ := json.Marshal(r)
		h.Write(b)
		clear(b)
		records = append(records, r)
	}
	release()
	if err != nil {
		return out, values.VaultError(ctx, err)
	}
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i].Item, records[j].Item
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID < b.ID
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	revision := hex.EncodeToString(h.Sum(nil))
	offset := 0
	if cursor != "" {
		if len(cursor) > 512 {
			return out, failure("cursor_invalid")
		}
		var c listCursor
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || len(b) > 256 || vault.StrictDecode(b, &c) != nil || c.Revision != revision || c.Offset < 0 || c.Offset >= len(records) {
			return out, failure("cursor_invalid")
		}
		offset = c.Offset
	}
	end := min(offset+limit, len(records))
	for _, r := range records[offset:end] {
		item, err := s.observed(ctx, r)
		if err != nil {
			return out, values.VaultError(ctx, err)
		}
		out.Items = append(out.Items, item)
	}
	if end < len(records) {
		b, _ := json.Marshal(listCursor{revision, end})
		next := base64.RawURLEncoding.EncodeToString(b)
		out.NextCursor = &next
	}
	return out, nil
}
