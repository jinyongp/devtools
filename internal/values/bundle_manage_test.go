package values

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestManagedBundlesAtomicReplayAndImportMembership(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	n := 0
	apply := func(c Change) ChangeResult {
		t.Helper()
		view, e := store.Inspect(ctx, "")
		if e != nil {
			t.Fatal(e)
		}
		n++
		c.Revision = view.Revision
		c.RequestID = fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
		r, e := store.Apply(ctx, c)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	apply(Change{Action: "import", Import: &BatchImport{Content: "URL=https://example.invalid\nOTHER=second\n", Variables: []string{"URL", "OTHER"}, Overwrite: true}})
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	c := Change{Action: "bundle.set", Bundle: &BundleChange{Name: "frontend", Members: []string{"URL"}}, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000999"}
	first, e := store.Apply(ctx, c)
	if e != nil || !first.Changed {
		t.Fatal(first, e)
	}
	replay, e := store.Apply(ctx, c)
	if e != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatal("bundle replay", e)
	}
	c.Bundle = &BundleChange{Name: "frontend", Members: []string{"OTHER"}}
	if _, e := store.Apply(ctx, c); e == nil || e.Code != "request_conflict" {
		t.Fatal("changed UUID input", e)
	}
	before, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	invalid := Change{Action: "bundle.set", Bundle: &BundleChange{Name: "frontend", Members: []string{"URL", "MISSING"}}, Revision: before.Revision, RequestID: "00000000-0000-4000-8000-000000000998"}
	if _, e := store.Apply(ctx, invalid); e == nil || e.Code != "key_not_found" {
		t.Fatal("invalid membership", e)
	}
	after, e := store.Inspect(ctx, "")
	if e != nil || after.Revision != before.Revision {
		t.Fatal("partial bundle publish", e)
	}
	id := after.Bundles[0].ID
	apply(Change{Action: "bundle.rename", Bundle: &BundleChange{Name: "frontend", NewName: "web"}})
	after, e = store.Inspect(ctx, "")
	if e != nil || after.Bundles[0].ID != id || len(after.Bundles[0].ValueIDs) != 1 || after.Items[0].ValueID == "" {
		t.Fatal("bundle metadata", e)
	}
	// Adding unchanged values to a bundle is a real membership mutation even
	// though it does not change any value version.
	added := apply(Change{Action: "import", Import: &BatchImport{Content: "OTHER=second\n", Variables: []string{"OTHER"}, Bundle: "web"}})
	if !added.Changed || added.Bundle != "web" {
		t.Fatal("membership-only import lost")
	}
	snap, e := CapturePublic(ctx, root, "app", "", true, time.Now(), "web")
	if e != nil {
		t.Fatal(e)
	}
	defer snap.Destroy()
	if len(snap.Metadata().Slots) != 2 {
		t.Fatal("import did not union shared membership")
	}
	current, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	preview := apply(Change{Action: "import", Import: &BatchImport{Content: "NEW=preview\n", Variables: []string{"NEW"}, Bundle: "web", Preview: true}})
	last, e := store.Inspect(ctx, "")
	if e != nil || last.Revision != current.Revision || preview.Changed {
		t.Fatal("preview mutated", e)
	}
	apply(Change{Action: "bundle.remove", Bundle: &BundleChange{Name: "web"}})
	last, e = store.Inspect(ctx, "")
	if e != nil || len(last.Bundles) != 0 || len(last.Items) != 2 {
		t.Fatal("bundle removal deleted original values", e)
	}
}
