package values

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestDeliveredInputComparesEffectiveSlotsAndIncarnation(t *testing.T) {
	root, store, master, key := vaultSnapshotFixture(t, 2)
	ctx := context.Background()
	update := func(f func(*vault.Profile) (bool, error)) {
		t.Helper()
		if _, err := store.Update(ctx, "app", key, f); err != nil {
			t.Fatal(err)
		}
	}
	update(func(p *vault.Profile) (bool, error) { return p.SetBundle("backend", []string{"SECRET_0000"}, key) })
	spec := InputSpec{Selection: "backend", Inject: true}
	capture := func(env string) *InputVersion {
		t.Helper()
		var calls atomic.Int32
		s, e := CaptureVault(ctx, root, "app", env, true, time.Now(), snapshotMaster(master, &calls), spec.Selection)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Destroy()
		v, err := NewInputVersion(s.Metadata(), env, spec)
		if err != nil || v == nil {
			t.Fatal("missing version", err)
		}
		return v
	}
	first := capture("local")
	requireSame := func(want bool, a, b *InputVersion) {
		t.Helper()
		same, known := SameInput(a, b)
		if !known || same != want {
			t.Fatalf("same=%v known=%v want=%v", same, known, want)
		}
	}
	requireSame(true, first, capture("")) // common slot under two env selections
	update(func(p *vault.Profile) (bool, error) {
		return p.Set("SECRET_0000", vault.Secret, "", "synthetic-slot-value-0000", key)
	})
	requireSame(true, first, capture("local")) // identical assignment preserves version
	update(func(p *vault.Profile) (bool, error) {
		return p.Set("SECRET_0001", vault.Secret, "", "synthetic-unrelated-change", key)
	})
	requireSame(true, first, capture("local"))
	update(func(p *vault.Profile) (bool, error) {
		return p.Set("SECRET_0000", vault.Secret, "local", "synthetic-slot-value-0000", key)
	})
	requireSame(false, first, capture("local")) // equal text in a different effective slot
	requireSame(true, first, capture(""))
	override := capture("local")
	update(func(p *vault.Profile) (bool, error) {
		return p.Set("SECRET_0000", vault.Secret, "", "synthetic-shadowed-common", key)
	})
	requireSame(true, override, capture("local"))
	update(func(p *vault.Profile) (bool, error) {
		return p.SetBundle("backend", []string{"SECRET_0000", "SECRET_0001"}, key)
	})
	requireSame(false, override, capture("local"))
	renamed := override.Clone()
	renamed.KeyEpoch++
	renamed.IntegrityVerified = false
	renamed.Spec.Selection = "renamed"
	renamed.Env = "display-only"
	requireSame(true, override, renamed)
	restored := override.Clone()
	restored.Identity.StorageGeneration = "replacement-generation"
	requireSame(false, override, restored)
	encoded, err := json.Marshal(first)
	if err != nil || strings.Contains(string(encoded), "synthetic-slot-value") || strings.Contains(string(encoded), "ticket") || strings.Contains(string(encoded), "session_id") {
		t.Fatal("delivery metadata contains value or authority", err)
	}
}

func TestDeliveredInputFiltersBindingsShadowedValuesAndUnknown(t *testing.T) {
	source := SnapshotMetadata{Identity: vault.InputIdentity{ProfileID: "profile", StorageGeneration: "generation", Revision: strings.Repeat("0", 64)}, Slots: []vault.SlotReference{{Name: "A", SlotID: "a", Version: "1"}, {Name: "B", SlotID: "b", Version: "1"}, {Name: "C", SlotID: "c", Version: "1"}}}
	for _, tc := range []struct {
		name string
		spec InputSpec
		want string
	}{
		{"all", InputSpec{Inject: true}, "ABC"},
		{"binding only", InputSpec{Bindings: []string{"B", "B"}}, "B"},
		{"shadowed", InputSpec{Inject: true, Shadowed: []string{"A", "B"}}, "C"},
		{"shadowed but used by another binding", InputSpec{Inject: true, Shadowed: []string{"A", "B"}, Bindings: []string{"A"}}, "AC"},
		{"none", InputSpec{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := NewInputVersion(source, "", tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			names := ""
			for _, s := range v.Slots {
				names += s.Name
			}
			if names != tc.want {
				t.Fatalf("got %q want %q", names, tc.want)
			}
			copy := v.Clone()
			copy.Spec.Bindings = append(copy.Spec.Bindings, "MUTATED")
			if len(copy.Slots) > 0 {
				copy.Slots[0].Version = "mutated"
			}
			if len(v.Slots) > 0 && v.Slots[0].Version != "1" {
				t.Fatal("clone aliases delivery")
			}
		})
	}
	legacy, err := NewInputVersion(SnapshotMetadata{}, "", InputSpec{Inject: true})
	if err != nil || legacy != nil {
		t.Fatal("legacy snapshot became known", err)
	}
	empty, err := NewInputVersion(SnapshotMetadata{}, "", InputSpec{})
	if err != nil || empty == nil || empty.Slots == nil {
		t.Fatal("known empty input lost", err)
	}
	if _, known := SameInput(legacy, empty); known {
		t.Fatal("unknown equated with empty")
	}
}
