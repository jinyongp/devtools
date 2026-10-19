package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
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
