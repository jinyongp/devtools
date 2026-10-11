package truststore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	s := System{GOOS: "darwin", Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
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

func TestDarwinUntrustRemovesOnlySelectedCertificateAndItsTrust(t *testing.T) {
	a, _, e := (ca.Store{Data: t.TempDir()}).Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	keychain := "/Library/Keychains/System.keychain"
	for _, tc := range []struct {
		name, output string
		failure      error
		wantDelete   bool
	}{
		{"present", "SHA-256 hash: " + strings.ToUpper(a.Fingerprint()), nil, true},
		{"absent", "SHA-256 hash: " + strings.Repeat("0", 64), nil, false},
		{"delete failure", "SHA-256 hash: " + a.Fingerprint(), errors.New("keychain locked"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deleted := false
			s := System{GOOS: "darwin", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "/usr/bin/security" {
					t.Fatalf("unexpected executable %q", name)
				}
				if slices.Equal(args, []string{"find-certificate", "-a", "-Z", keychain}) {
					return []byte(tc.output), nil
				}
				// Apple's -t removes this item's trust settings as well as the
				// certificate; the exact SHA-256 hash preserves sibling CAs.
				if !slices.Equal(args, []string{"delete-certificate", "-t", "-Z", a.Fingerprint(), keychain}) {
					t.Fatalf("unscoped certificate removal: %v", args)
				}
				deleted = true
				return nil, tc.failure
			}}
			err := s.Apply(context.Background(), Request{Action: "untrust", Certificate: a.PEM()})
			if !errors.Is(err, tc.failure) || deleted != tc.wantDelete {
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
