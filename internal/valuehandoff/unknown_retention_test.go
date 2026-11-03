package valuehandoff

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestPrepareReplayPreservesUncertainDeliveryPayload(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	input := PrepareInput{ExternalExecutionID: "unknown-job", Selection: Selection{Keys: []string{"TOKEN"}}}
	prepared, e := s.Prepare(ctx, input, requestID(7000))
	if e != nil {
		t.Fatal(e)
	}
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
	r.Item.DeliveryState, r.Item.ExecutionState = "unknown", "unknown"
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
	payload := filepath.Join(s.Root, filepath.FromSlash(s.path("value-execution-payloads", r.Item.ID)))
	before, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	replayed, e := s.Prepare(ctx, input, requestID(7000))
	if e != nil || !replayed.Replayed {
		t.Fatal(e)
	}
	after, err := os.ReadFile(payload)
	if err != nil || string(before) != string(after) {
		t.Fatal("uncertain payload was removed or replaced", err)
	}
}
