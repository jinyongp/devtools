package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestProtectedRecoveryAndVerificationMaterialAreBoundToTheirDomain(t *testing.T) {
	ctx := context.Background()
	master, _ := NewKey()
	defer master.Destroy()
	other, _ := NewKey()
	defer other.Destroy()
	vaultID, _ := NewID()
	scope, _ := NewID()
	plain := NewMaterial([]byte("synthetic-private-legacy-mac-key-and-old-values"))
	defer plain.Destroy()
	for _, purpose := range []string{MaterialRecovery, MaterialLegacyVerification, MaterialReceipt} {
		encoded, err := SealProtected(ctx, master, vaultID, scope, purpose, plain)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, plain.data) {
			t.Fatal("protected document contains raw material")
		}
		decoded, err := OpenProtected(ctx, master, vaultID, scope, purpose, encoded)
		if err != nil || !sameMaterial(decoded, plain) {
			t.Fatal("protected recovery mismatch", err)
		}
		decoded.Destroy()
		if _, err = OpenProtected(ctx, other, vaultID, scope, purpose, encoded); !errors.Is(err, ErrIntegrity) {
			t.Fatal("wrong root accepted", err)
		}
		otherScope, _ := NewID()
		if _, err = OpenProtected(ctx, master, vaultID, otherScope, purpose, encoded); !errors.Is(err, ErrIntegrity) {
			t.Fatal("wrong scope accepted", err)
		}
		if _, err = OpenProtected(ctx, master, vaultID, scope, "wrong", encoded); !errors.Is(err, ErrIntegrity) {
			t.Fatal("wrong purpose accepted", err)
		}
		var envelope protectedMaterial
		json.Unmarshal(encoded, &envelope)
		envelope.Nonce[0] ^= 1
		corrupt, _ := json.Marshal(envelope)
		if _, err = OpenProtected(ctx, master, vaultID, scope, purpose, corrupt); !errors.Is(err, ErrIntegrity) {
			t.Fatal("nonce modification accepted", err)
		}
	}
	if _, err := SealProtected(ctx, master, vaultID, scope, "unknown", plain); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown protection domain accepted", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := SealProtected(canceled, master, vaultID, scope, MaterialRecovery, plain); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
