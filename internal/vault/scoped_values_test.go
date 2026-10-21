package vault

import (
	"errors"
	"testing"
	"time"
)

func TestScopedSelectionChecksCeilingBeforeOpeningAndSkipsUnselectedSecrets(t *testing.T) {
	p, key := profileFixture(t)
	if _, err := p.SetBundle("api", []string{"TOKEN", "URL"}, key); err != nil {
		t.Fatal(err)
	}
	// Keep a valid whole-profile manifest with an independently invalid AEAD
	// slot. Selecting URL must authenticate metadata without opening TOKEN.
	for i := range p.Values {
		if p.Values[i].Name == "TOKEN" {
			p.Values[i].Slots[0].Ciphertext[0] ^= 1
		}
	}
	if err := p.Sign(key); err != nil {
		t.Fatal(err)
	}
	if got, _, err := p.ResolveScopedValues("", key, time.Now(), []string{"URL"}, ""); err != nil || len(got) != 1 || got[0].Reference.Name != "URL" {
		t.Fatal("unselected secret opened", err)
	}
	if _, _, err := p.ResolveScopedValues("", key, time.Now(), []string{"URL"}, "api"); err == nil || err.Error() != "selection_outside_scope" {
		t.Fatal("scope ceiling checked after decryption", err)
	}
	if _, _, err := p.ResolveScopedValues("", key, time.Now(), []string{"TOKEN"}, ""); !errors.Is(err, ErrIntegrity) {
		t.Fatal("selected invalid ciphertext not opened", err)
	}
	if _, _, err := p.ResolveScopedValues("", key, time.Now(), []string{}, "all"); err == nil || err.Error() != "selection_outside_scope" {
		t.Fatal("all bundle bypassed empty ceiling", err)
	}
	if got, _, err := p.ResolveScopedValues("", key, time.Now(), []string{}, "none"); err != nil || len(got) != 0 {
		t.Fatal("none selected values", err)
	}
}

func TestScopedSelectionRejectsExpiredOverrideAndMissingEffectiveSlot(t *testing.T) {
	p, key := profileFixture(t)
	for i := range p.Values {
		if p.Values[i].Name == "TOKEN" {
			for j := range p.Values[i].Slots {
				if p.Values[i].Slots[j].Env == "local" {
					expiry := time.Now().Add(-time.Second).Unix()
					p.Values[i].Slots[j].ExpiresAt = &expiry
				}
			}
		}
	}
	if err := p.Sign(key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.ResolveScopedValues("local", key, time.Now(), []string{"TOKEN"}, ""); err == nil || err.Error() != "value_expired" {
		t.Fatal("expired override fell back to common", err)
	}
	if _, err := p.Set("LOCAL_ONLY", Secret, "local", "synthetic-local-only", key); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetBundle("local-only", []string{"LOCAL_ONLY"}, key); err != nil {
		t.Fatal(err)
	}
	for _, bundle := range []string{"", "local-only"} {
		if _, _, err := p.ResolveScopedValues("", key, time.Now(), []string{"LOCAL_ONLY"}, bundle); err == nil || err.Error() != "key_not_found" {
			t.Fatal("missing effective slot accepted", err)
		}
	}
}
