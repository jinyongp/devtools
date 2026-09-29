package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
)

func TestExportSnapshotV3MatchesReplayReference(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "portable"}))
	call(t, store, "task.update", task, Object{"description": "changed"})

	release, err := maintenance.Acquire(context.Background(), maintenance.Root(store.Directory))
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	actual, err := store.ExportSnapshotHeld(128 << 20)
	if err != nil {
		t.Fatal(err)
	}
	journal, state, _, loadErr := store.loadResolved()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if err := canonicalizeJournalReceipts(journal, state); err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual.Data, expected) {
		t.Fatal("v3 fast export changed logical journal bytes")
	}
}

func TestCanonicalizeExportJournalFallsBackForLegacyReceiptShape(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "portable"}))
	call(t, store, "task.update", task, Object{"description": "changed"})

	journal, _, _, loadErr := store.loadResolved()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	var requestID string
	for id, receipt := range journal.Receipts {
		requestID = id
		result := copyObject(receipt.Result)
		delete(result, "affected_ids")
		delete(result, "affected_count")
		receipt.Result = result
		journal.Receipts[id] = receipt
		break
	}
	if requestID == "" {
		t.Fatal("fixture did not create a receipt")
	}
	if fast, err := canonicalizeJournalReceiptsFast(journal); err != nil || fast {
		t.Fatalf("legacy-shaped receipt did not require fallback: fast=%v err=%v", fast, err)
	}
	if err := canonicalizeExportJournal(journal); err != nil {
		t.Fatal(err)
	}
	result := journal.Receipts[requestID].Result
	if _, ok := result["affected_ids"]; !ok {
		t.Fatal("fallback did not restore affected_ids")
	}
	if _, ok := result["affected_count"]; !ok {
		t.Fatal("fallback did not restore affected_count")
	}
}
