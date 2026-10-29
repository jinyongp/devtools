package tasks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func TestQueryCachePruneBoundsActualJSONReads(t *testing.T) {
	for _, prefix := range []string{
		`{"projection_version":1,"fingerprint":"known","expires":"2000-01-01T00:00:00Z","revision":1,"items":["`,
		`{"version":1,"projection_version":1,"profile":"test","kind":"task","expires":"2000-01-01T00:00:00Z","revision":1,"nodes":{"item":"`,
	} {
		// Match the supported projection without coupling the payload size to the
		// parser's buffering. The actual file position measures OS bytes read.
		prefix = strings.Replace(prefix, `"projection_version":1`, `"projection_version":`+strconv.Itoa(ProjectionVersion), 1)
		path := filepath.Join(privateTempDir(t), ID()+".json")
		suffix := `"]}`
		if strings.Contains(prefix, `"nodes"`) {
			suffix = `"}}`
		}
		body := prefix + strings.Repeat("x", 2*queryCacheHeader) + suffix
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		f, err := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		expires, owned := queryCacheExpiration(f, strings.TrimSuffix(filepath.Base(path), ".json"))
		position, err := f.Seek(0, 1)
		f.Close()
		if err != nil || !owned || expires.IsZero() || position <= 0 || position > queryCacheHeader {
			t.Fatal("unbounded metadata read", position, owned, err)
		}
		t.Logf("metadata bytes read=%d, payload bytes=%d", position, len(body))
	}
}
