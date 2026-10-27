package tasks

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
)

const queryCacheMagic = "DTQUERY2"
const queryCacheHeader = 64 << 10
const queryCacheIndex = 16 + queryCacheHeader
const queryCacheChunk = 32
const queryCacheChunkBytes = 128 << 20

type queryCacheHeaderV2 struct {
	Version     int       `json:"version"`
	Profile     string    `json:"profile"`
	Projection  int       `json:"projection_version"`
	Fingerprint string    `json:"fingerprint"`
	Revision    int       `json:"revision"`
	Expires     time.Time `json:"expires"`
	Total       int       `json:"total"`
	ChunkSize   int       `json:"chunk_size"`
	IndexOffset int64     `json:"index_offset"`
}
type queryCachePart struct {
	Fingerprint string `json:"fingerprint"`
	Revision    int    `json:"revision"`
	Start       int    `json:"start"`
	Items       []any  `json:"items"`
}

func decodeQueryCache(body []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("invalid query cache tail")
	}
	return nil
}

func writeQueryCache(path, profile string, snapshot Snapshot) error {
	if len(snapshot.Items) == 0 || snapshot.Expires.IsZero() {
		return errors.New("invalid query snapshot")
	}
	h := queryCacheHeaderV2{2, profile, snapshot.Projection, snapshot.Fingerprint, snapshot.Revision, snapshot.Expires, len(snapshot.Items), queryCacheChunk, queryCacheIndex}
	header, err := json.Marshal(h)
	if err != nil {
		return err
	}
	if len(header) > queryCacheHeader {
		return errors.New("query header too large")
	}
	chunks := (len(snapshot.Items) + queryCacheChunk - 1) / queryCacheChunk
	body := make([]byte, queryCacheIndex+16*chunks)
	copy(body, queryCacheMagic)
	binary.LittleEndian.PutUint64(body[8:16], uint64(len(header)))
	copy(body[16:], header)
	for n := 0; n < chunks; n++ {
		start := n * queryCacheChunk
		end := min(start+queryCacheChunk, len(snapshot.Items))
		part, err := json.Marshal(queryCachePart{snapshot.Fingerprint, snapshot.Revision, start, snapshot.Items[start:end]})
		if err != nil {
			return err
		}
		if len(part) > queryCacheChunkBytes {
			return errors.New("query chunk too large")
		}
		entry := body[queryCacheIndex+16*n : queryCacheIndex+16*(n+1)]
		binary.LittleEndian.PutUint64(entry, uint64(len(body)))
		binary.LittleEndian.PutUint64(entry[8:], uint64(len(part)))
		body = append(body, part...)
	}
	r, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.WriteFile(context.Background(), filepath.Base(path), body, false)
}

func queryHeader(f *platformfs.File) (queryCacheHeaderV2, int64, error) {
	var h queryCacheHeaderV2
	info, err := f.Stat()
	if err != nil {
		return h, 0, err
	}
	var prelude [16]byte
	if _, err = f.ReadAt(prelude[:], 0); err != nil {
		return h, 0, err
	}
	length := binary.LittleEndian.Uint64(prelude[8:])
	if string(prelude[:8]) != queryCacheMagic || length == 0 || length > queryCacheHeader {
		return h, 0, errors.New("invalid query prelude")
	}
	body := make([]byte, int(length))
	if _, err = f.ReadAt(body, 16); err != nil {
		return h, 0, err
	}
	if err = decodeQueryCache(body, &h); err != nil {
		return h, 0, err
	}
	if h.Version != 2 || h.Total < 1 || h.Total > math.MaxInt-queryCacheChunk || h.ChunkSize != queryCacheChunk || h.IndexOffset != queryCacheIndex || h.Expires.IsZero() {
		return h, 0, errors.New("invalid query header")
	}
	chunks := (int64(h.Total) + queryCacheChunk - 1) / queryCacheChunk
	if chunks > (math.MaxInt64-queryCacheIndex)/16 || queryCacheIndex+16*chunks > info.Size() {
		return h, 0, errors.New("invalid query index size")
	}
	if _, _, err := queryEntry(f, h, info.Size(), int(chunks)-1); err != nil {
		return h, 0, err
	}
	return h, info.Size(), nil
}

func queryEntry(f *platformfs.File, h queryCacheHeaderV2, size int64, n int) (int64, int64, error) {
	chunks := (h.Total + queryCacheChunk - 1) / queryCacheChunk
	if n < 0 || n >= chunks {
		return 0, 0, errors.New("invalid query chunk index")
	}
	var entry [16]byte
	if _, err := f.ReadAt(entry[:], h.IndexOffset+int64(n)*16); err != nil {
		return 0, 0, err
	}
	offset, length := binary.LittleEndian.Uint64(entry[:8]), binary.LittleEndian.Uint64(entry[8:])
	minimum := uint64(queryCacheIndex) + 16*uint64(chunks)
	if offset < minimum || offset > uint64(size) || length == 0 || length > queryCacheChunkBytes || length > uint64(size)-offset {
		return 0, 0, errors.New("invalid query chunk bounds")
	}
	if n == 0 && offset != minimum || n == chunks-1 && offset+length != uint64(size) {
		return 0, 0, errors.New("invalid query file bounds")
	}
	return int64(offset), int64(length), nil
}

func readQueryPage(f *platformfs.File, profile, fingerprint, token string, offset, limit int) (Object, error) {
	h, size, err := queryHeader(f)
	if err != nil {
		return nil, err
	}
	if h.Profile != profile || h.Projection != ProjectionVersion || h.Fingerprint != fingerprint || !time.Now().Before(h.Expires) || offset < 0 || offset > h.Total {
		return nil, errors.New("invalid query identity")
	}
	end := offset + min(limit, h.Total-offset)
	items := []any{}
	for n := offset / queryCacheChunk; n < (end+queryCacheChunk-1)/queryCacheChunk; n++ {
		at, length, err := queryEntry(f, h, size, n)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			prev, plen, e := queryEntry(f, h, size, n-1)
			if e != nil || prev+plen != at {
				return nil, errors.New("invalid query adjacency")
			}
		}
		if n+1 < (h.Total+queryCacheChunk-1)/queryCacheChunk {
			next, _, e := queryEntry(f, h, size, n+1)
			if e != nil || at+length != next {
				return nil, errors.New("invalid query adjacency")
			}
		}
		body := make([]byte, int(length))
		if _, err = f.ReadAt(body, at); err != nil {
			return nil, err
		}
		var part queryCachePart
		if err = decodeQueryCache(body, &part); err != nil {
			return nil, err
		}
		start := n * queryCacheChunk
		count := min(queryCacheChunk, h.Total-start)
		if part.Fingerprint != h.Fingerprint || part.Revision != h.Revision || part.Start != start || len(part.Items) != count {
			return nil, errors.New("invalid query chunk identity")
		}
		items = append(items, part.Items[max(0, offset-start):min(count, end-start)]...)
	}
	out := Object{"profile": profile, "revision": h.Revision, "items": items, "next_cursor": nil}
	if end < h.Total {
		out["next_cursor"] = token + ":" + strconv.Itoa(end)
	}
	return out, nil
}

// QueryCacheExpiry reads either the bounded v2 header or a legacy JSON snapshot.
// Cleanup preserves the complete raw file as its existing single archive unit.
func QueryCacheExpiry(path string) (time.Time, error) {
	f, err := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	var magic [8]byte
	if _, err = f.ReadAt(magic[:], 0); err != nil {
		return time.Time{}, err
	}
	if string(magic[:]) == queryCacheMagic {
		h, _, err := queryHeader(f)
		return h.Expires, err
	}
	var legacy struct {
		Expires time.Time `json:"expires"`
	}
	info, err := f.Stat()
	if err != nil || info.Size() > 128<<20 {
		return time.Time{}, errors.New("legacy query too large")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 128<<20+1))
	if err = decoder.Decode(&legacy); err != nil {
		return time.Time{}, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return time.Time{}, errors.New("invalid legacy query tail")
	}
	return legacy.Expires, nil
}
