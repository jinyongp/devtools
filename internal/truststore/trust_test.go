package truststore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestIsolatedTrustRejectsBeforeRunner(t *testing.T) {
	m := Manager{Isolated: true, Run: func(context.Context, string, any, bool) *protocol.Error { t.Fatal("OS runner called"); return nil }}
	if e := m.Apply(context.Background(), "trust", nil); e == nil || e.Code != "isolated_operation_not_allowed" {
		t.Fatal(e)
	}
}
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
func TestOwnedTrustPathDoesNotFollowSymlink(t *testing.T) {
	a, _, e := (ca.Store{Data: t.TempDir()}).Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	directory := t.TempDir()
	external := filepath.Join(t.TempDir(), "external")
	if e := os.WriteFile(external, []byte("preserve"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(external, filepath.Join(directory, "devtools-"+a.Fingerprint()+".crt")); e != nil {
		t.Fatal(e)
	}
	s := System{GOOS: "linux", Directory: directory, Tool: "unused", Run: func(context.Context, string, ...string) ([]byte, error) { t.Fatal("runner called"); return nil, nil }}
	if e := s.Apply(context.Background(), Request{Action: "untrust", Certificate: a.PEM()}); e == nil {
		t.Fatal("followed symlink")
	}
	b, e2 := os.ReadFile(external)
	if e2 != nil || string(b) != "preserve" {
		t.Fatal(e2)
	}
}
