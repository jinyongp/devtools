package vault

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBundlesShareIdentityAndAtomicMembership(t *testing.T) {
	p, key := profileFixture(t)
	valuesBefore := append([]Value{}, p.Values...)
	if changed, err := p.SetBundle("api", []string{"TOKEN", "URL"}, key); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := p.SetBundle("frontend", []string{"URL"}, key); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if !reflect.DeepEqual(valuesBefore, p.Values) {
		t.Fatal("membership copied or changed values")
	}
	id := p.Bundles[0].ID
	before, _ := p.Encode()
	if changed, err := p.SetBundle("api", []string{"URL", "TOKEN"}, key); err != nil || changed {
		t.Fatal("order-only change", changed, err)
	}
	for _, names := range [][]string{{"URL", "MISSING"}, {"URL", "URL"}} {
		if _, err := p.SetBundle("api", names, key); err == nil {
			t.Fatal("invalid bulk membership accepted")
		}
		after, _ := p.Encode()
		if !bytes.Equal(before, after) {
			t.Fatal("failed membership partially applied")
		}
	}
	if _, err := p.RenameBundle("api", "backend", key); err != nil {
		t.Fatal(err)
	}
	if p.Bundles[0].ID != id {
		t.Fatal("rename changed bundle identity")
	}
	if _, err := p.Set("URL", Public, "", "https://updated.invalid", key); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"backend", "frontend"} {
		got, err := p.ResolveSelectedValues("local", key, time.Now(), true, name)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, value := range got {
			if value.Reference.Name == "URL" {
				found = value.Value == "https://updated.invalid" && value.Reference.ValueID == valuesBefore[1].ID
			}
		}
		if !found {
			t.Fatal("shared value update missing")
		}
	}
	if _, err := p.RemoveBundle("backend", key); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ResolveSelectedValues("local", key, time.Now(), true, "backend"); err == nil || err.Error() != "bundle_not_found" {
		t.Fatal("missing bundle fell back", err)
	}
	if len(p.Values) != len(valuesBefore) {
		t.Fatal("removing bundle removed values")
	}
}

func TestBundleSelectionSeparatesEnvExpiryAndManifest(t *testing.T) {
	p, key := profileFixture(t)
	if _, err := p.SetBundle("frontend", []string{"URL"}, key); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetBundle("empty", nil, key); err != nil {
		t.Fatal(err)
	}
	for i := range p.Values {
		if p.Values[i].Name == "TOKEN" {
			for j := range p.Values[i].Slots {
				if p.Values[i].Slots[j].Env == "local" {
					expiry := time.Now().Add(-time.Hour).Unix()
					p.Values[i].Slots[j].ExpiresAt = &expiry
				}
			}
		}
	}
	if err := p.Sign(key); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{"none", "empty", "frontend"} {
		got, err := p.ResolveSelectedValues("local", key, time.Now(), true, selection)
		if err != nil {
			t.Fatal(selection, err)
		}
		want := 0
		if selection == "frontend" {
			want = 1
		}
		if len(got) != want {
			t.Fatal("selection expanded", selection, len(got))
		}
	}
	for _, selection := range []string{"", "all"} {
		if _, err := p.ResolveSelectedValues("local", key, time.Now(), true, selection); err == nil || err.Error() != "value_expired" {
			t.Fatal("expired override fell back", err)
		}
	}
	if _, err := p.ResolveSelectedValues("missing", key, time.Now(), true, "none"); err == nil || err.Error() != "env_not_found" {
		t.Fatal("selection bypassed env validation", err)
	}
	// No selection, even none, authenticates only a subset of the manifest.
	p.Bundles[0].ValueIDs = nil
	if _, err := p.ResolveSelectedValues("", key, time.Now(), true, "none"); !errors.Is(err, ErrIntegrity) {
		t.Fatal("none bypassed manifest authentication", err)
	}
}

func TestPublicBundlesAndReservedNames(t *testing.T) {
	p, err := NewProfile("app")
	if err != nil {
		t.Fatal(err)
	}
	p.Set("URL", Public, "", "https://example.invalid", nil)
	for _, name := range []string{"all", "none", "", "nested/api"} {
		if _, err := p.SetBundle(name, []string{"URL"}, nil); err == nil {
			t.Fatal("reserved/invalid bundle accepted")
		}
	}
	if _, err := p.SetBundle("web", []string{"URL"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := p.ResolveSelectedValues("", nil, time.Now(), true, "web")
	if err != nil || len(got) != 1 || got[0].Reference.Name != "URL" {
		t.Fatal(got, err)
	}
}
