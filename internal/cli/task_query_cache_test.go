package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestQueryCacheSyntaxPrecedesProfileAndStorage(t *testing.T) {
	for _, path := range []string{"", "relative"} {
		a := New("test", "test")
		a.dataDirectory = func() (string, *protocol.Error) { t.Fatal("invalid cache accessed data path"); return "", nil }
		var out, err bytes.Buffer
		code := a.Run(context.Background(), []string{"task", "next", "--profile", "app", "--query-cache-dir", path}, IO{Out: &out, Err: &err})
		if code != 2 || out.Len() != 0 {
			t.Fatal(code, out.String(), err.String())
		}
	}
}

func TestQueryCacheOptionOnlyOnQueries(t *testing.T) {
	a := New("test", "test")
	var out, err bytes.Buffer
	if code := a.Run(context.Background(), []string{"schema", "--all"}, IO{Out: &out, Err: &err}); code != 0 {
		t.Fatal(err.String())
	}
	var envelope struct {
		Data struct {
			Commands []struct {
				Name    string `json:"name"`
				Options []struct {
					Name string `json:"name"`
				} `json:"options"`
			} `json:"commands"`
		} `json:"data"`
	}
	if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, c := range envelope.Data.Commands {
		for _, o := range c.Options {
			if o.Name == "query-cache-dir" {
				count++
			}
		}
	}
	if count != 22 {
		t.Fatal("query-only option coverage", count)
	}
	out.Reset()
	err.Reset()
	code := a.Run(context.Background(), []string{"task", "add", "--title", "bad", "--request-id", "00000000-0000-4000-8000-000000000000", "--query-cache-dir", filepath.Join(privateTempDir(t), "cache")}, IO{Out: &out, Err: &err})
	if code != 2 || out.Len() != 0 {
		t.Fatal("mutation accepted query cache", code, err.String())
	}
}
