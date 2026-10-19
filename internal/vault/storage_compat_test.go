package vault

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestStorageV1DerivationIndependentOfSessionProtocol(t *testing.T) {
	bytes := make([]byte, 32)
	for i := range bytes {
		bytes[i] = byte(i)
	}
	key := NewMaterial(bytes)
	clear(bytes)
	defer key.Destroy()
	got, err := derive(key, strings.Repeat("1", 32), strings.Repeat("2", 32), "encryption")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(got)
	// Independently calculated with Python stdlib HMAC/HKDF using the documented
	// DTV1 key-info encoding and storage version 1. Transport upgrades must not
	// change existing keysets, catalog MACs, or ciphertext decryption keys.
	const want = "4f8dca2522fe3b86ac56d6dd56b0b14822e86c0f629ed9cfd60a60511c058dd9"
	if hex.EncodeToString(got) != want {
		t.Fatal("storage v1 crypto contract changed with IPC version")
	}
}
