package idref

import (
	"context"
	"slices"
	"testing"
)

func TestResolve(t *testing.T) {
	ctx := context.Background()
	first := "a4531000-0000-4000-8000-000000000001"
	second := "a453b000-0000-4000-8000-000000000002"
	keys := slices.Values([]string{second, first})
	for _, input := range []string{"A4531", "a4531000-00", first} {
		id, e := Resolve(ctx, UUID, input, keys)
		if e != nil || id != first {
			t.Fatalf("%q: %q %v", input, id, e)
		}
	}
	for input, code := range map[string]string{"a45": "invalid_argument", "a453": "ambiguous_id", "a454": "id_not_found"} {
		id, e := Resolve(ctx, UUID, input, keys)
		if id != "" || e == nil || e.Code != code {
			t.Fatalf("%q: %q %v", input, id, e)
		}
		if code == "ambiguous_id" {
			got := e.Details["facts"].(map[string]any)["suggested_ids"].([]string)
			if !slices.Equal(got, []string{"a4531", "a453b"}) {
				t.Fatal(got)
			}
		}
	}
	id, e := Resolve(ctx, UUID, first, nil)
	if e != nil || id != first {
		t.Fatal(id, e)
	}
	hex := "0123456789abcdef0123456789abcdef"
	id, e = Resolve(ctx, Hex, "0123", slices.Values([]string{hex}))
	if e != nil || id != hex {
		t.Fatal(id, e)
	}
}
