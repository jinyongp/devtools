package expose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthenticationCommitmentsAreKeyedAndStrict(t *testing.T) {
	s := Store{Data: t.TempDir()}
	ctx := context.Background()
	a, e := s.Commitment(ctx, "11111111-1111-4111-8111-111111111111", "user:canary", false)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Commitment(ctx, "11111111-1111-4111-8111-111111111111", "user:canary", false)
	if e != nil || a != b {
		t.Fatal(e)
	}
	c, _ := s.Commitment(ctx, "11111111-1111-4111-8111-111111111111", "user:different", false)
	d, _ := s.Commitment(ctx, "11111111-1111-4111-8111-111111111111", "", true)
	if a == c || a == d {
		t.Fatal("auth change kept commitment")
	}
	other, _ := (Store{Data: t.TempDir()}).Commitment(ctx, "11111111-1111-4111-8111-111111111111", "user:canary", false)
	if a == other {
		t.Fatal("commitment is not host-keyed")
	}
	key := filepath.Join(s.Data, "exposure-auth", "key.json")
	data, _ := os.ReadFile(key)
	if strings.Contains(string(data), "canary") {
		t.Fatal("raw auth persisted")
	}
	if e := os.WriteFile(key, []byte(`"invalid"`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Commitment(ctx, "11111111-1111-4111-8111-111111111111", "user:canary", false); e == nil || e.Code != "auth_commitment_key_invalid" {
		t.Fatal(e)
	}
	if e := os.Remove(key); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Commitment(ctx, "11111111-1111-4111-8111-111111111111", "user:canary", false); e == nil || e.Code != "auth_commitment_key_missing" {
		t.Fatal(e)
	}
	for _, auth := range []string{"", ":password", "username:", "user:pass\n", "raw-password"} {
		if ValidateAuth(auth, false) == nil {
			t.Fatalf("accepted invalid auth %q", auth)
		}
	}
}
