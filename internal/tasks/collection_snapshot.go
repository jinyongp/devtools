package tasks

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retention"
)

// CollectionPager reuses the private, indexed query-cache format and its native
// containment/ownership checks. Item zero is public collection metadata; the
// remaining items are public rows. Continuations read only bounded chunks.
// No task journal or active data root is opened by this adapter.
type CollectionPager struct{ DataRoot, CacheDirectory string }

func (p CollectionPager) store() (Store, *protocol.Error) {
	s := Store{Directory: filepath.Join(p.DataRoot, "tasks"), Profile: "collection-query"}
	return s.withQueryCache(p.CacheDirectory)
}

func (p CollectionPager) First(ctx context.Context, fingerprint string, metadata Object, rows []any, limit int) (Object, *protocol.Error) {
	if limit < 1 || limit > 200 {
		return nil, failure("invalid_argument", "Use limit between 1 and 200.")
	}
	s, e := p.store()
	if e != nil {
		return nil, e
	}
	out := Object{}
	for k, v := range metadata {
		out[k] = v
	}
	end := min(limit, len(rows))
	out["items"], out["next_cursor"] = rows[:end], nil
	if end == len(rows) {
		return out, nil
	}
	if err := s.prepareQueryCache(); err != nil {
		return nil, protocol.WithStorageDetails(protocol.NewError("query_cache_unavailable", "Use a writable private --query-cache-dir separate from data, then repeat the query.", 1, nil), "collections", "prepare_query_cache", "query_cache", err)
	}
	token := ID()
	snapshot := Snapshot{Projection: ProjectionVersion, Fingerprint: fingerprint, Expires: time.Now().Add(retention.QuerySnapshotTTL), Items: append([]any{metadata}, rows...)}
	if err := writeQueryCache(filepath.Join(p.CacheDirectory, token+".json"), s.Profile, snapshot); err != nil {
		return nil, protocol.WithStorageDetails(protocol.NewError("query_cache_unavailable", "Cannot publish the private collection snapshot.", 1, nil), "collections", "write_query_cache", "query_cache", err)
	}
	s.pruneQueryCaches(ctx)
	out["next_cursor"] = token + ":" + strconv.Itoa(end+1)
	return out, nil
}

func (p CollectionPager) Continue(fingerprint, cursor string, limit int) (Object, *protocol.Error) {
	invalid := func() (Object, *protocol.Error) {
		return nil, failure("cursor_invalid", "Start the same collection query again; its snapshot is missing, expired or belongs to other conditions.")
	}
	if limit < 1 || limit > 200 {
		return nil, failure("invalid_argument", "Use limit between 1 and 200.")
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 || !validID(parts[0]) {
		return invalid()
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil || offset < 1 {
		return invalid()
	}
	s, e := p.store()
	if e != nil {
		return nil, e
	}
	f, err := s.openQueryCache(parts[0] + ".json")
	if err != nil {
		return invalid()
	}
	defer f.Close()
	header, err := readQueryPage(f, s.Profile, fingerprint, parts[0], 0, 1)
	if err != nil {
		return invalid()
	}
	first, ok := header["items"].([]any)
	if !ok || len(first) != 1 {
		return invalid()
	}
	metadata, ok := first[0].(map[string]any)
	if !ok {
		return invalid()
	}
	page, err := readQueryPage(f, s.Profile, fingerprint, parts[0], offset, limit)
	if err != nil {
		return invalid()
	}
	out := Object{}
	for k, v := range metadata {
		out[k] = v
	}
	out["items"], out["next_cursor"] = page["items"], page["next_cursor"]
	return out, nil
}
