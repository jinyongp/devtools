package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/jinyongp/devtools/internal/process"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	info             os.FileInfo
	key              [32]byte
	reason, observed string
}
type Cache struct {
	mu      sync.Mutex
	entries []cacheEntry
}
type cacheKey struct{}

func WithCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, cacheKey{}, &Cache{})
}

var versions = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+(?:[-+_][0-9A-Za-z.+_-]+)?`)

func matchesVersion(output, expected string) bool { return versions.FindString(output) == expected }
func probe(ctx context.Context, path string, args []string, dir string, environment []string) (string, string) {
	info, _ := os.Stat(path)
	raw, _ := json.Marshal([]any{args, dir, environment})
	key := sha256.Sum256(raw)
	cache, _ := ctx.Value(cacheKey{}).(*Cache)
	if cache != nil {
		cache.mu.Lock()
		for _, e := range cache.entries {
			if e.key == key && info != nil && e.info != nil && os.SameFile(info, e.info) && info.Size() == e.info.Size() && info.ModTime() == e.info.ModTime() {
				cache.mu.Unlock()
				return e.reason, e.observed
			}
		}
		cache.mu.Unlock()
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output := &limitedOutput{}
	code, e := process.Execute(bounded, append([]string{path}, args...), dir, environment, nil, output, output)
	reason, observed := "version_token", versions.FindString(output.data.String())
	if len(observed) > 128 {
		observed = ""
	}
	switch {
	case ctx.Err() != nil:
		reason = "deadline_exceeded"
		if errors.Is(ctx.Err(), context.Canceled) {
			reason = "canceled"
		}
	case bounded.Err() != nil:
		reason = "timeout"
	case output.truncated:
		reason = "output_limit"
	case e != nil || code != 0:
		reason = "probe_failed"
	case observed == "":
		reason = "version_unreadable"
	}
	if reason != "version_token" {
		observed = ""
	}
	if after, err := os.Stat(path); ctx.Err() == nil && (err != nil || info == nil || !os.SameFile(info, after) || info.Size() != after.Size() || info.ModTime() != after.ModTime()) {
		reason, observed = "executable_changed", ""
	}
	if cache != nil && ctx.Err() == nil {
		cache.mu.Lock()
		cache.entries = append(cache.entries, cacheEntry{info: info, key: key, reason: reason, observed: observed})
		cache.mu.Unlock()
	}
	return reason, observed
}

type limitedOutput struct {
	mu        sync.Mutex
	data      strings.Builder
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := (16 << 10) - b.data.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.data.Write(p)
	return n, nil
}
