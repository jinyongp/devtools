//go:build !windows

package truststore

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/ca"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedCAFileAndUpdaterResumeAfterFailure(t *testing.T) {
	a, _, e := (ca.Store{Data: t.TempDir()}).Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	directory := t.TempDir()
	calls := 0
	s := System{GOOS: "linux", Directory: directory, Tool: "fake-update", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls++
		if name != "fake-update" {
			t.Fatal(name)
		}
		if calls == 1 {
			return nil, errors.New("interrupted updater")
		}
		return nil, nil
	}}
	r := Request{Action: "trust", Certificate: a.PEM()}
	if err := s.Apply(context.Background(), r); err == nil {
		t.Fatal("failure hidden")
	}
	path := filepath.Join(directory, "devtools-"+a.Fingerprint()+".crt")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(context.Background(), r); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	r.Action = "untrust"
	if err := s.Apply(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
