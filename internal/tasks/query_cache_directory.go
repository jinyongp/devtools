package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

// ValidateQueryCachePath performs no filesystem access. Call before profile
// resolution so malformed cache options cannot initialize unrelated storage.
func ValidateQueryCachePath(path string) *protocol.Error {
	if path == "" || !filepath.IsAbs(path) {
		return failure("invalid_argument", "query-cache-dir requires an absolute private directory path.")
	}
	if _, err := platformfs.ValidatePrivatePath(path); err != nil {
		return failure("invalid_argument", "query-cache-dir requires an absolute private directory path.")
	}
	return nil
}

func pathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	contains := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	}
	return contains(a, b) || contains(b, a)
}

func (s Store) withQueryCache(path string) (Store, *protocol.Error) {
	if e := ValidateQueryCachePath(path); e != nil {
		return s, e
	}
	if pathsOverlap(path, maintenance.Root(s.Directory)) {
		return s, failure("invalid_argument", "query-cache-dir must be separate from the data root and its ancestors.")
	}
	s.Cache, s.queryCacheExplicit = filepath.Clean(path), true
	return s, nil
}

// Resolve only for overlap detection before writing. Cursor reads deliberately
// do not inspect the active data root. OpenRoot separately rejects cache aliases.
func existingPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = existingPath(parent)
	return filepath.Join(resolved, filepath.Base(path)), err
}

func (s Store) prepareQueryCache() error {
	if !s.queryCacheExplicit {
		return PrivateDir(s.cacheDirectory())
	}
	data, err := existingPath(maintenance.Root(s.Directory))
	if err != nil {
		return err
	}
	cache, err := existingPath(s.cacheDirectory())
	if err != nil {
		return err
	}
	if pathsOverlap(data, cache) {
		return platformfs.ErrUnsafe
	}
	// Include directory aliases that are not represented by symlinks.
	for _, pair := range [][2]string{{data, cache}, {cache, data}} {
		anchor, err := os.Stat(pair[0])
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if anchor == nil {
			continue
		}
		for p := pair[1]; ; p = filepath.Dir(p) {
			info, err := os.Stat(p)
			if err == nil && os.SameFile(anchor, info) {
				return platformfs.ErrUnsafe
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if filepath.Dir(p) == p {
				break
			}
		}
	}
	r, err := platformfs.OpenRoot(s.cacheDirectory(), true)
	if err != nil {
		return err
	}
	return r.Close()
}

func (s Store) openQueryCache(name string) (*platformfs.File, error) {
	if !s.queryCacheExplicit {
		return platformfs.OpenPrivate(filepath.Join(s.cacheDirectory(), name), os.O_RDONLY, 0)
	}
	r, err := platformfs.OpenRoot(s.cacheDirectory(), false)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.OpenFile(name, os.O_RDONLY)
}

func decodeQuerySnapshot(reader io.Reader, value any) error {
	d := json.NewDecoder(reader)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("invalid query snapshot tail")
	}
	return nil
}

func (s Store) readQuerySnapshot(name string, value any) error {
	f, err := s.openQueryCache(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return decodeQuerySnapshot(f, value)
}

func (s Store) writeGraphCache(ctx context.Context, name string, snapshot GraphSnapshot) error {
	if !s.queryCacheExplicit {
		return WritePrivate(filepath.Join(s.cacheDirectory(), name), snapshot)
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	r, err := platformfs.OpenRoot(s.cacheDirectory(), false)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.WriteFile(ctx, name, body, false)
}

func (s Store) queryCacheError(q Query, stage string, cause error) *protocol.Error {
	if e := protocol.ContextError(cause); e != nil {
		return e
	}
	e := storageErrorAt(stage, "query_cache", cause)
	argv := append([]string{"devtools", "task"}, strings.Fields(q.Command)...)
	if q.Target != "" {
		argv = append(argv, q.Target)
	}
	argv = append(argv, "--profile", s.Profile)
	keys := []string{}
	for key := range q.Options {
		if key != "profile" && key != "query-cache-dir" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		argv = append(argv, "--"+key)
		if key != "stdin" {
			argv = append(argv, q.Options[key])
		}
	}
	argv = append(argv, "--query-cache-dir", "<ABSOLUTE_PRIVATE_CACHE_DIRECTORY>")
	e.Details["remedies"] = []protocol.Remedy{{Relation: protocol.SameStoreAction, Argv: argv, Effect: "value_write", RequiredInputs: []string{"ABSOLUTE_PRIVATE_CACHE_DIRECTORY"}, Message: "Retry the original query with a writable private query-cache-dir separate from task data. Use that directory for every continuation."}}
	return e
}
