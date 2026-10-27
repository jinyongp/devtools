package valuehandoff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestRetentionHeldAuthenticatesMetadataWithoutDecryptingPayload(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	prepared, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "inventory-job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(850))
	if e != nil {
		t.Fatal(e)
	}
	inspection, err := vault.PrepareInspection(ctx, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	lease, err := inspection.Lease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if lease != nil {
			lease()
		}
	}()
	gate, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if gate != nil {
			gate()
		}
	}()
	r, err := s.readRecord(ctx, a, prepared.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Item.ExpiresAt = time.Now().Add(-time.Hour)
	path := s.path("value-executions", r.Item.ID)
	body, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Replace(s.Root, map[string][]byte{path: body}); err != nil {
		t.Fatal(err)
	}
	out := retention.Inventory{}
	err = InspectRetentionHeld(ctx, inspection, "app", &out)
	if err != nil || len(out.Buckets) != 3 {
		t.Fatal(out, err)
	}
	var bytes int64
	for _, b := range out.Buckets {
		bytes += b.Bytes
		if b.Kind == "execution_payload" && (b.Lifecycle != "expired" || b.Verification != "record_verified" || b.CleanupEligible) {
			t.Fatal(b)
		}
	}
	var expected int64
	for _, domain := range []string{"value-executions", "value-execution-payloads", "value-execution-requests"} {
		id := prepared.Item.ID
		if domain == "value-execution-requests" {
			id = requestID(850)
		}
		info, err := os.Stat(filepath.Join(s.Root, s.path(domain, id)))
		if err != nil {
			t.Fatal(err)
		}
		expected += info.Size()
	}
	if bytes != expected {
		t.Fatal("physical bytes counted more than once", bytes, expected)
	}
	budget, cancel := context.WithCancel(ctx)
	cancel()
	if err := InspectRetentionHeld(budget, inspection, "app", &retention.Inventory{}); err == nil {
		t.Fatal("incomplete scan succeeded")
	}
	gate()
	gate = nil
	lease()
	lease = nil
	if err := c.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if err := inspection.CheckHeld(); err == nil {
		t.Fatal("prepared inspection survived manual lock")
	}
}
