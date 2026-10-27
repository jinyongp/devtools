package values

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestReceiptRetentionCountsLegacyCopiesOnceLogically(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	s := Store{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	view, e := s.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	id := "00000000-0000-4000-8000-000000000101"
	value := "public"
	if _, e := s.Apply(ctx, Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: id}); e != nil {
		t.Fatal(e)
	}
	canonical := filepath.Join(root, s.receiptRelative(id))
	body, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "backup-receipts", "values-"+hex.EncodeToString([]byte(s.Profile))+"-"+id+".json")
	inspection, err := vault.PrepareInspection(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	key, err := PrepareReceiptInspection(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	for _, state := range []string{"legacy_no_expiry", "spent"} {
		var r vault.ValueReceipt
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatal(err)
		}
		if state == "legacy_no_expiry" {
			r.CreatedAt = 0
			r.ExpiresAt = 0
		} else {
			r.CreatedAt = time.Now().Add(-48 * time.Hour).Unix()
			r.ExpiresAt = time.Now().Add(-time.Hour).Unix()
		}
		encoded, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{canonical, legacy} {
			if err := maintenance.Write(path, encoded); err != nil {
				t.Fatal(err)
			}
		}
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		out := retention.Inventory{}
		err = InspectRetentionHeld(ctx, inspection, key, "app", &out)
		release()
		if err != nil || len(out.Buckets) != 1 {
			t.Fatal(out, err)
		}
		b := out.Buckets[0]
		if b.Files != 2 || b.LogicalItems != 1 || b.Bytes != int64(2*len(encoded)) || b.Lifecycle != state || b.CleanupEligible {
			t.Fatal(b)
		}
	}
}
