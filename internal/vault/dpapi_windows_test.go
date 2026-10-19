package vault

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestNativeDPAPIUserRoundtrip(t *testing.T) {
	p := DPAPIProtector{VaultID: "0123456789abcdef0123456789abcdef"}
	key := bytes.Repeat([]byte{0x47}, 32)
	wrapped, err := p.Wrap(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrapped, key) {
		t.Fatal("plaintext key persisted")
	}
	got, err := p.Unwrap(context.Background(), wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("roundtrip", err)
	}
	clear(got)
	other := DPAPIProtector{VaultID: "1123456789abcdef0123456789abcdef"}
	if _, err := other.Unwrap(context.Background(), wrapped); err == nil {
		t.Fatal("cross-vault unwrap")
	}
	for _, i := range []int{0, len(wrapped) / 2, len(wrapped) - 1} {
		bad := bytes.Clone(wrapped)
		bad[i] ^= 1
		if _, err := p.Unwrap(context.Background(), bad); err == nil {
			t.Fatal("corruption accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Unwrap(ctx, wrapped); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
