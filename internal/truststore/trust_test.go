package truststore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestDarwinTrustAuthorizationIsBoundedAndHonorsCancellation(t *testing.T) {
	a, _, e := (ca.Store{Data: t.TempDir()}).Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := System{GOOS: "darwin", Directory: t.TempDir(), Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Minute {
			t.Fatal("unbounded system authorization")
		}
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	if err := s.Apply(ctx, Request{Action: "untrust", Certificate: a.PEM()}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDarwinUntrustRejectsSelectedCAThenRemovesItsKeychainItem(t *testing.T) {
	a, _, e := (ca.Store{Data: t.TempDir()}).Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	keychain := "/Library/Keychains/System.keychain"
	for _, tc := range []struct {
		name        string
		denyError   error
		deleteError error
	}{
		{"success", nil, nil},
		{"deny failure", errors.New("authorization denied"), nil},
		{"delete failure", nil, errors.New("keychain locked")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			denied, deleted := false, false
			s := System{GOOS: "darwin", Directory: t.TempDir(), Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "/usr/bin/security" {
					t.Fatalf("unexpected executable %q", name)
				}
				if len(args) == 7 && slices.Equal(args[:6], []string{"add-trusted-cert", "-d", "-r", "deny", "-k", keychain}) {
					data, err := os.ReadFile(args[6])
					if err != nil || string(data) != string(a.PEM()) {
						t.Fatalf("wrong CA rejection: %v", err)
					}
					denied = true
					return nil, tc.denyError
				}
				if !denied || !slices.Equal(args, []string{"delete-certificate", "-Z", a.Fingerprint(), keychain}) {
					t.Fatalf("unscoped certificate removal: %v", args)
				}
				deleted = true
				return nil, tc.deleteError
			}}
			err := s.Apply(context.Background(), Request{Action: "untrust", Certificate: a.PEM()})
			wantError := tc.denyError
			if wantError == nil {
				wantError = tc.deleteError
			}
			if !errors.Is(err, wantError) || !denied || deleted != (tc.denyError == nil) {
				t.Fatal(err, deleted)
			}
		})
	}
}

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
