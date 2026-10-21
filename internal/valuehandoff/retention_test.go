package valuehandoff

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestExpiredPrepareReplayRetiresSnapshotButPreservesResult(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	input := PrepareInput{ExternalExecutionID: "retention-job", Selection: Selection{Keys: []string{"TOKEN"}}}
	prepared, e := s.Prepare(ctx, input, requestID(800))
	if e != nil {
		t.Fatal(e)
	}
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	gate, err := s.lockCommit(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.readRecord(ctx, a, prepared.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Item.ExpiresAt = time.Now().Add(-time.Second)
	path := s.path("value-executions", r.Item.ID)
	b, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Replace(s.Root, map[string][]byte{path: b}); err != nil {
		t.Fatal(err)
	}
	gate()
	payload := filepath.Join(s.Root, s.path("value-execution-payloads", r.Item.ID))
	view, e := s.Status(ctx, r.Item.ID, "")
	if e != nil || view.Item.DeliveryState != "expired" {
		t.Fatal("expiry observation", e)
	}
	if _, err := os.Stat(payload); err != nil {
		t.Fatal("read-only status erased data", err)
	}
	if err := c.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, e := s.Prepare(ctx, input, requestID(800)); e == nil || e.Code != "vault_locked" {
		t.Fatal("locked cleanup opened a key", e)
	}
	if _, err := os.Stat(payload); err != nil {
		t.Fatal("locked replay erased snapshot", err)
	}
	pass := vault.NewMaterial([]byte("synthetic-handoff-passphrase"))
	defer pass.Destroy()
	if err := c.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	replay, e := s.Prepare(ctx, input, requestID(800))
	if e != nil || !replay.Replayed || replay.Item != prepared.Item {
		t.Fatal("cleanup rewrote prepare outcome", e)
	}
	if _, err := os.Stat(payload); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("authenticated replay retained snapshot", err)
	}
	view, e = s.Status(ctx, r.Item.ID, "")
	if e != nil || view.Item.Revision != 2 || view.Item.DeliveryState != "expired" {
		t.Fatal("retirement record", e)
	}
	if err := c.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if summary := InspectSummary(ctx, s.Root); summary.Verification != "verification_pending" || summary.Total != nil {
		t.Fatal("locked diagnostic invented counts")
	}
}

func TestAggregateAuthenticatesProfileBindingsWithoutIdleRenewal(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	if _, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "aggregate-job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(801)); e != nil {
		t.Fatal(e)
	}
	before, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	summary := InspectSummary(ctx, s.Root)
	if summary.Verification != "verified" || summary.Total == nil || *summary.Total != 1 || *summary.PendingInputs != 1 {
		t.Fatal("verified aggregate", summary)
	}
	after, err := c.Status(ctx)
	if err != nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("aggregate renewed idle", err)
	}
	b, _ := json.Marshal(summary)
	for _, forbidden := range []string{"aggregate-job", "input_id", "ticket", "patterns", "canary"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatal("aggregate exposed metadata")
		}
	}
}

func TestAggregateNeverCountsUnverifiedRecords(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	r, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "corruption-job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(802))
	if e != nil {
		t.Fatal(e)
	}
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	gate, err := s.lockCommit(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	path := s.path("value-executions", r.Item.ID)
	b, err := maintenance.Read(filepath.Join(s.Root, path), vault.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 1
	if err := maintenance.Replace(s.Root, map[string][]byte{path: b}); err != nil {
		t.Fatal(err)
	}
	gate()
	if summary := InspectSummary(ctx, s.Root); summary.Verification != "verification_pending" || summary.Total != nil {
		t.Fatal("unverified metadata counted")
	}
}
