package tasks

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

// queryCacheExpiration recognizes only our metadata. It never decodes the
// unbounded item/node payload, including for old JSON snapshots.
func queryCacheExpiration(f *platformfs.File, token string) (time.Time, bool) {
	var magic [8]byte
	if _, err := f.ReadAt(magic[:], 0); err == nil && string(magic[:]) == queryCacheMagic {
		h, _, err := queryHeader(f)
		return h.Expires, err == nil && h.Token == token && h.Projection == ProjectionVersion && h.Profile != "" && h.Fingerprint != ""
	}
	d := json.NewDecoder(io.LimitReader(f, queryCacheHeader))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return time.Time{}, false
	}
	meta := struct {
		Projection  int       `json:"projection_version"`
		Fingerprint string    `json:"fingerprint"`
		Expires     time.Time `json:"expires"`
		Revision    int       `json:"revision"`
		Version     int       `json:"version"`
		Profile     string    `json:"profile"`
		Kind        string    `json:"kind"`
	}{}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			break
		}
		name, ok := key.(string)
		if !ok {
			break
		}
		if name == "items" || name == "nodes" {
			body, err := json.Marshal(fields)
			if err != nil || json.Unmarshal(body, &meta) != nil {
				break
			}
			if meta.Projection != ProjectionVersion || meta.Expires.IsZero() || meta.Revision < 0 || fields["revision"] == nil {
				break
			}
			if name == "items" && meta.Fingerprint != "" && len(fields) == 4 {
				return meta.Expires, true
			}
			if name == "nodes" && meta.Version == graphSnapshotVersion && meta.Profile != "" && (meta.Kind == "task" || meta.Kind == "workstream") && len(fields) == 6 {
				return meta.Expires, true
			}
			break
		}
		switch name {
		case "projection_version", "fingerprint", "expires", "revision", "version", "profile", "kind":
			if fields[name] != nil {
				return time.Time{}, false
			}
			var value json.RawMessage
			if d.Decode(&value) != nil {
				return time.Time{}, false
			}
			fields[name] = value
		default:
			return time.Time{}, false
		}
	}
	return time.Time{}, false
}

func (s Store) pruneQueryCaches(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	r, err := platformfs.OpenRoot(s.cacheDirectory(), false)
	if err != nil {
		return
	}
	defer r.Close()
	entries, err := os.ReadDir(s.cacheDirectory())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		name := entry.Name()
		token := strings.TrimSuffix(name, ".json")
		if !strings.HasSuffix(name, ".json") || !validID(token) {
			continue
		}
		f, err := r.OpenFile(name, os.O_RDONLY)
		if err != nil {
			continue
		}
		expires, owned := queryCacheExpiration(f, token)
		_ = f.Close()
		if owned && time.Now().After(expires) {
			_ = r.RemoveFile(ctx, name)
		}
	}
}
