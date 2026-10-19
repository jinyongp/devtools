package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeychainStatusContract(t *testing.T) {
	for code, want := range map[int32]error{0: nil, -25300: ErrKeyMissing, -25308: ErrInteractionRequired, -128: ErrDenied, -25293: ErrDenied, -25299: ErrRecovery, -25291: ErrUnavailable} {
		if !errors.Is(keychainStatus(code), want) {
			t.Fatalf("status %d", code)
		}
	}
}

func TestNativeKeychainSyntheticVault(t *testing.T) {
	if os.Getenv("DEVTOOLS_TEST_NATIVE_KEYCHAIN") != "1" {
		t.Skip("requires disposable macOS runner/user keychain")
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	p := KeychainProtector{VaultID: id}
	key := bytes.Repeat([]byte{0x39}, 32)
	wrapped, err := p.Wrap(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Remove(context.Background()); err != nil && !errors.Is(err, ErrKeyMissing) {
			t.Error("synthetic item cleanup", err)
		}
	})
	if bytes.Contains(wrapped, key) {
		t.Fatal("plaintext key persisted")
	}
	got, err := p.Unwrap(context.Background(), wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("roundtrip", err)
	}
	clear(got)
	if _, err := p.Wrap(context.Background(), bytes.Repeat([]byte{0x41}, 32)); !errors.Is(err, ErrRecovery) {
		t.Fatal("duplicate replaced", err)
	}
	got, err = p.Unwrap(context.Background(), wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("duplicate changed key", err)
	}
	clear(got)
	otherID, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	other := KeychainProtector{VaultID: otherID}
	if _, err := other.Unwrap(context.Background(), wrapped); err == nil {
		t.Fatal("cross-vault locator")
	}
	if err := p.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Unwrap(context.Background(), wrapped); !errors.Is(err, ErrKeyMissing) {
		t.Fatal("missing key", err)
	}
}

// This test may lock only the dedicated keychain selected by the disposable
// runner. Production callers never create or lock an OS collection implicitly.
func TestNativeKeychainLockedCollectionRefusesSilentAccess(t *testing.T) {
	if os.Getenv("DEVTOOLS_DISPOSABLE_HOST") != "1" || os.Getenv("DEVTOOLS_TEST_NATIVE_KEYCHAIN") != "1" || os.Getenv("DEVTOOLS_TEST_KEYCHAIN") == "" {
		t.Skip("requires disposable dedicated keychain")
	}
	path := os.Getenv("DEVTOOLS_TEST_KEYCHAIN")
	if !filepath.IsAbs(path) || filepath.Base(path) != "synthetic.keychain-db" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "devtools-native-keychain-") {
		t.Fatal("keychain must belong to the dedicated fixture")
	}
	security := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// No key material or real credential is put in argv/output. Empty password
		// belongs only to the synthetic keychain created by check-macos-vault.py.
		return exec.CommandContext(ctx, "/usr/bin/security", args...).Run()
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	p := KeychainProtector{VaultID: id}
	key := bytes.Repeat([]byte{0x52}, 32)
	wrapped, err := p.Wrap(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := security("unlock-keychain", "-p", "", path); err != nil {
			t.Error("cannot unlock synthetic keychain for cleanup")
			return
		}
		if err := p.Remove(context.Background()); err != nil {
			t.Error("synthetic item cleanup", err)
		}
	})
	if err = security("lock-keychain", path); err != nil {
		t.Fatal("cannot lock synthetic keychain")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := p.Unwrap(ctx, wrapped)
	clear(got)
	if !errors.Is(err, ErrInteractionRequired) && !errors.Is(err, ErrDenied) {
		t.Fatal("locked collection allowed silent access or returned an unclassified error", err)
	}
	if err = security("unlock-keychain", "-p", "", path); err != nil {
		t.Fatal("cannot unlock synthetic keychain")
	}
	got, err = p.Unwrap(context.Background(), wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("unlock did not preserve the original key", err)
	}
	clear(got)
}
