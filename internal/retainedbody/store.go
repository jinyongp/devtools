// Package retainedbody stores immutable, compressed bodies shared by replay
// receipts and archives. References retain exact bytes; no replay deadline is
// shortened. Owners, not this store, decide when a reference can be removed.
package retainedbody

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

const MaxBytes = 128 << 20
const magic = "DTRBODY1"

type Reference struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Digest  string `json:"digest"`
	Bytes   int64  `json:"bytes"`
}

type Store struct{ Root string }

func (s Store) Publication(ctx context.Context) (func(), error) {
	return maintenance.LockShared(ctx, filepath.Join(s.Root, ".retained-bodies.lock"))
}
func (s Store) Query(ctx context.Context) (func(), error) {
	return maintenance.LockPassiveShared(ctx, filepath.Join(s.Root, ".retained-bodies.lock"))
}

func validKind(kind string) bool { return kind == "process_record" || kind == "archive_payload" }
func (r Reference) Valid() bool {
	b, e := hex.DecodeString(r.Digest)
	return r.Version == 1 && validKind(r.Kind) && e == nil && len(b) == 32 && r.Digest == strings.ToLower(r.Digest) && r.Bytes >= 0 && r.Bytes <= MaxBytes
}
func digest(kind string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(kind + "\x00"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
func (s Store) directory() string { return filepath.Join(s.Root, "retained-bodies") }

func (s Store) Put(ctx context.Context, kind string, body []byte) (Reference, error) {
	r := Reference{Version: 1, Kind: kind, Bytes: int64(len(body))}
	if !filepath.IsAbs(s.Root) || !validKind(kind) || len(body) > MaxBytes {
		return r, errors.New("invalid retained body")
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	r.Digest = digest(kind, body)
	root, err := platformfs.OpenRoot(s.directory(), true)
	if err != nil {
		return r, err
	}
	defer root.Close()
	if file, err := root.OpenFile(r.Digest, os.O_RDONLY); err == nil {
		file.Close()
		existing, err := s.Read(r)
		if err != nil {
			return r, err
		}
		if !bytes.Equal(existing, body) {
			return r, errors.New("retained body identity mismatch")
		}
		return r, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return r, err
	}
	var encoded bytes.Buffer
	encoded.WriteString(magic)
	writer, err := gzip.NewWriterLevel(&encoded, gzip.BestSpeed)
	if err != nil {
		return r, err
	}
	if _, err := writer.Write(body); err != nil {
		writer.Close()
		return r, err
	}
	if err := writer.Close(); err != nil {
		return r, err
	}
	if err := root.WriteFile(ctx, r.Digest, encoded.Bytes(), false); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, readErr := s.Read(r)
			if readErr == nil && bytes.Equal(existing, body) {
				return r, nil
			}
		}
		return r, err
	}
	return r, nil
}

func (s Store) Read(r Reference) ([]byte, error) {
	if !filepath.IsAbs(s.Root) || !r.Valid() {
		return nil, errors.New("invalid retained reference")
	}
	root, err := platformfs.OpenRoot(s.directory(), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.OpenFile(r.Digest, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxBytes+(1<<20) {
		return nil, errors.New("retained body too large")
	}
	var header [len(magic)]byte
	if _, err := io.ReadFull(file, header[:]); err != nil || string(header[:]) != magic {
		return nil, errors.New("unsupported retained body format")
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, r.Bytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) != r.Bytes || digest(r.Kind, body) != r.Digest {
		return nil, errors.New("retained body integrity mismatch")
	}
	return body, nil
}
