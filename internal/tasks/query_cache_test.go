package tasks

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexedQueryPagesAndLegacyCompatibility(t *testing.T) {
	s := fixture(t)
	q := Query{Command: "list", Options: map[string]string{}}
	items := make([]any, 97)
	for i := range items {
		items[i] = Object{"title": fmt.Sprint(i)}
	}
	first, e := s.page(q, Object{"revision": 7}, items, 1)
	if e != nil {
		t.Fatal(e)
	}
	token := strings.Split(str(first, "next_cursor"), ":")[0]
	for _, offset := range []int{0, 1, 31, 32, 33, 96, 97} {
		for _, limit := range []int{1, 2, 32, 50} {
			q.Options = map[string]string{"cursor": fmt.Sprintf("%s:%d", token, offset), "limit": fmt.Sprint(limit)}
			out, e := s.Query(context.Background(), q)
			if e != nil {
				t.Fatal(offset, limit, e)
			}
			rows := out["items"].([]any)
			if len(rows) != min(limit, len(items)-offset) {
				t.Fatal(offset, limit, len(rows))
			}
			for i, row := range rows {
				if row.(map[string]any)["title"] != fmt.Sprint(offset+i) {
					t.Fatal("order changed")
				}
			}
		}
	}
	path := filepath.Join(s.cacheDirectory(), token+".json")
	legacy := Snapshot{Projection: ProjectionVersion, Fingerprint: queryFingerprint(s.Profile, q), Revision: 7, Expires: time.Now().Add(time.Minute), Items: items}
	if err := WritePrivate(path, legacy); err != nil {
		t.Fatal(err)
	}
	q.Options["cursor"] = token + ":32"
	if _, e := s.Query(context.Background(), q); e != nil {
		t.Fatal("legacy TTL cursor", e)
	}
	legacy.Expires = time.Now().Add(-time.Second)
	if err := WritePrivate(path, legacy); err != nil {
		t.Fatal(err)
	}
	if _, e := s.Query(context.Background(), q); e == nil || e.Code != "cursor_invalid" {
		t.Fatal("expired legacy accepted", e)
	}
}

func TestIndexedQueryRejectsCorruption(t *testing.T) {
	for _, kind := range []string{"truncated", "tail", "offset", "length", "header", "chunk", "expiry", "token"} {
		t.Run(kind, func(t *testing.T) {
			s := fixture(t)
			q := Query{Command: "list", Options: map[string]string{}}
			items := make([]any, 65)
			for i := range items {
				items[i] = Object{"title": "item"}
			}
			first, e := s.page(q, Object{"revision": 7}, items, 1)
			if e != nil {
				t.Fatal(e)
			}
			cursor := str(first, "next_cursor")
			token := strings.Split(cursor, ":")[0]
			path := filepath.Join(s.cacheDirectory(), token+".json")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "truncated":
				body = body[:len(body)-1]
			case "tail":
				body = append(body, 'x')
			case "offset":
				binary.LittleEndian.PutUint64(body[queryCacheIndex:], ^uint64(0))
			case "length":
				binary.LittleEndian.PutUint64(body[queryCacheIndex+8:], queryCacheChunkBytes+1)
			case "header":
				body[16] = 'x'
			case "chunk":
				body[queryCacheIndex+16*3] = 'x'
			case "expiry", "token":
				var header queryCacheHeaderV2
				if err := json.Unmarshal(body[16:16+binary.LittleEndian.Uint64(body[8:16])], &header); err != nil {
					t.Fatal(err)
				}
				if kind == "token" {
					header.Token = ID()
				} else {
					header.Expires = time.Now().Add(-time.Hour)
				}
				encoded, err := json.Marshal(header)
				if err != nil {
					t.Fatal(err)
				}
				clear(body[16:queryCacheIndex])
				copy(body[16:], encoded)
				binary.LittleEndian.PutUint64(body[8:16], uint64(len(encoded)))
			}
			if err := writePrivateFixture(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			q.Options = map[string]string{"cursor": cursor, "limit": "1"}
			if _, e := s.Query(context.Background(), q); e == nil || e.Code != "cursor_invalid" {
				t.Fatal(kind, e)
			}
		})
	}
}

func BenchmarkQueryCursorCardinality(b *testing.B) {
	for _, count := range []int{200, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			dir := filepath.Join(b.TempDir(), "private")
			if err := PrivateDir(dir); err != nil {
				b.Fatal(err)
			}
			s := Store{Directory: dir, Profile: "bench", Cache: dir}
			items := make([]any, count)
			for i := range items {
				items[i] = Object{"id": ID(), "description": strings.Repeat("x", 4096)}
			}
			q := Query{Command: "list", Options: map[string]string{}}
			first, e := s.page(q, Object{"revision": 7}, items, 1)
			if e != nil {
				b.Fatal(e)
			}
			q.Options = map[string]string{"cursor": str(first, "next_cursor"), "limit": "1"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, e := s.Query(context.Background(), q); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
