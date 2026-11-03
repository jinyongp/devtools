package valuehandoff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestRetentionCompactsResolvedMetadataAndKeepsUnknownPayload(t *testing.T) {
	s, client := fixture(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-31 * 24 * time.Hour)
	ids := []string{}
	for n, state := range []string{"reported_finished", "unknown"} {
		result, pe := s.Prepare(ctx, PrepareInput{ExternalExecutionID: state, Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(8100+n))
		if pe != nil {
			t.Fatal(pe)
		}
		ids = append(ids, result.Item.ID)
		a, err := s.access(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		gate, err := s.lockCommit(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.readRecord(ctx, a, result.Item.ID)
		if err != nil {
			t.Fatal(err)
		}
		r.Item.ExecutionState = state
		r.Item.EndedAt = &old
		r.Item.DeliveryState = "delivered"
		if state == "unknown" {
			r.Item.DeliveryState = "unknown"
		}
		path := s.path("value-executions", r.Item.ID)
		body, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, r)
		if err == nil {
			err = maintenance.Replace(s.Root, map[string][]byte{path: body})
		}
		gate()
		a.master.Destroy()
		if err != nil {
			t.Fatal(err)
		}
	}
	payload := filepath.Join(s.Root, filepath.FromSlash(s.path("value-execution-payloads", ids[1])))
	before, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Maintain(ctx, s.Root, s.Profile, 2, false)
	if err != nil || result.Examined != 2 || result.Changed != 1 {
		t.Fatal(result, err)
	}
	after, err := os.ReadFile(payload)
	if err != nil || string(after) != string(before) {
		t.Fatal("unknown payload changed", err)
	}
	items, pe := s.Navigation(ctx, true)
	if pe != nil || len(items.Items) != 1 || items.Items[0].ID != ids[1] {
		t.Fatal(items, pe)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	result, err = Maintain(ctx, s.Root, s.Profile, 2, false)
	if err != nil || !result.Pending || result.Changed != 0 {
		t.Fatal(result, err)
	}
}

func TestMissingDeliveryLockRequiresExplicitPreparation(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	r := record{Item: Execution{ID: id, DeliveryState: "delivering"}}
	if _, err := s.observed(ctx, r); err == nil {
		t.Fatal("read initialized delivery lock")
	}
	lock := filepath.Join(s.Root, "value-execution-locks", profilekey.Key(s.Profile), id+".lock")
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("read created lock", err)
	}
	prepared, pe := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(8200))
	if pe != nil {
		t.Fatal(pe)
	}
	engine := storageupgrade.Engine{Root: s.Root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}
	report, pe := engine.Fix(ctx)
	if pe != nil || !report.Ready {
		t.Fatal(report, pe)
	}
	r.Item.ID = prepared.Item.ID
	observed, err := s.observed(ctx, r)
	if err != nil || observed.DeliveryState != "unknown" {
		t.Fatal(observed, err)
	}
}

func TestBoundedRetentionRemembersResolutionAfterNewerDetailRetires(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-31 * 24 * time.Hour)
	ids := []string{}
	for n, state := range []string{"reported_failed", "reported_finished"} {
		prepared, condition := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "same-job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(8300+n))
		if condition != nil {
			t.Fatal(condition)
		}
		ids = append(ids, prepared.Item.ID)
		a, err := s.access(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		gate, err := s.lockCommit(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.readRecord(ctx, a, prepared.Item.ID)
		if err != nil {
			t.Fatal(err)
		}
		r.Item.CreatedAt = old.Add(time.Duration(n) * time.Hour)
		r.Item.EndedAt = &old
		r.Item.DeliveryState, r.Item.ExecutionState = "delivered", state
		path := s.path("value-executions", r.Item.ID)
		body, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, r)
		if err == nil {
			err = maintenance.Replace(s.Root, map[string][]byte{path: body})
		}
		gate()
		a.master.Destroy()
		if err != nil {
			t.Fatal(err)
		}
	}
	// At most one authenticated execution is read per pass. The second pass
	// through an older failure can use the newer accepted result's saved proof.
	for range 6 {
		result, err := Maintain(ctx, s.Root, s.Profile, 1, false)
		if err != nil || result.Examined > 1 {
			t.Fatal(result, err)
		}
	}
	for _, history := range []bool{false, true} {
		list, condition := s.Navigation(ctx, history)
		if condition != nil || len(list.Items) != 0 {
			t.Fatal("resolved failure reappeared after successor retirement", list, condition)
		}
	}
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	for _, id := range ids {
		r, err := s.readRecord(ctx, a, id)
		if err != nil || r.RetiredAt == nil {
			t.Fatal("resolved metadata did not retire", id, err)
		}
	}
}
