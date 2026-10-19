package vault

import (
	"bytes"
	"errors"
	"testing"
)

func TestOSKeyPayloadBindsBackendVaultAndContents(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef"
	key := bytes.Repeat([]byte{0x71}, 32)
	b, err := osKeyPayload("dpapi", id, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := osKeyFromPayload("dpapi", id, b)
	if err != nil || !bytes.Equal(key, got) {
		t.Fatal("roundtrip", err)
	}
	for i := range b {
		bad := bytes.Clone(b)
		bad[i] ^= 1
		if _, err := osKeyFromPayload("dpapi", id, bad); !errors.Is(err, ErrIntegrity) {
			t.Fatalf("byte %d: %v", i, err)
		}
	}
	for _, identity := range [][2]string{{"keychain", id}, {"dpapi", "1123456789abcdef0123456789abcdef"}} {
		if _, err := osKeyFromPayload(identity[0], identity[1], b); !errors.Is(err, ErrIntegrity) {
			t.Fatal("identity accepted", err)
		}
	}
	if _, err := osKeyFromPayload("dpapi", id, b[:len(b)-1]); !errors.Is(err, ErrIntegrity) {
		t.Fatal("truncation", err)
	}
}
