package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func legacySensitiveJournal(signature string, withReceipt bool) (*Journal, string, string) {
	taskID := ID()
	runID := ID()
	requestID := ID()
	events := []Event{
		{ID: ID(), Sequence: 1, At: stamp(), Action: "profile.upgraded", Data: Object{
			"version": JournalVersion, "baseline": map[string]*DefinitionBasis{},
		}},
		{ID: ID(), Sequence: 2, At: stamp(), Action: "task.add", Target: taskID, Data: Object{"title": "legacy"}},
		{ID: ID(), Sequence: 3, At: stamp(), RequestID: requestID, Action: "run.claimed", Target: taskID, Data: Object{
			"run_id": runID, "directory": "/tmp",
		}},
	}
	receipts := map[string]Receipt{}
	if withReceipt {
		receipts[requestID] = Receipt{
			Fingerprint: "legacy-fingerprint",
			Result: Object{
				"item": Object{"definition_signature": signature},
				"run":  Object{"definition_signature": signature},
			},
		}
	}
	return &Journal{
		Version: JournalVersion, Profile: "test", Events: events,
		Receipts: receipts, Contexts: map[string]string{},
	}, taskID, runID
}

func TestReplayJournalUsesReceiptDefinitionSignatureWithoutChangingHistory(t *testing.T) {
	const expected = "receipt-definition-signature"
	journal, _, runID := legacySensitiveJournal(expected, true)
	state, err := replayJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if state.Runs[runID].Signature != expected {
		t.Fatalf("run signature = %q", state.Runs[runID].Signature)
	}
	if str(state.Events[2].Data, "definition_signature") != "" {
		t.Fatal("receipt enrichment changed logical event history")
	}
	atRevision, revisionErr := state.AtRevision(state.Revision)
	if revisionErr != nil || atRevision.Runs[runID].Signature != expected {
		t.Fatalf("at-revision signature = %q err=%v", atRevision.Runs[runID].Signature, revisionErr)
	}
}

func TestReplayJournalKeepsReceiptlessSignatureFallback(t *testing.T) {
	journal, _, runID := legacySensitiveJournal("", false)
	state, err := replayJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if state.Runs[runID].Signature == "" {
		t.Fatal("receipt-less legacy event lost compatibility signature")
	}
	if str(state.Events[2].Data, "definition_signature") != "" {
		t.Fatal("fallback changed logical event history")
	}
}

func TestV3CurrentAndHistoryReplayUseReceiptDefinitionSignature(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := PrivateDir(root); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "tasks")
	if err := PrivateDir(directory); err != nil {
		t.Fatal(err)
	}
	resolution, err := createGeneration(directory, "test", ID())
	if err != nil {
		t.Fatal(err)
	}

	taskID := ID()
	runID := ID()
	initial := []Event{
		{ID: ID(), Sequence: 1, At: stamp(), Action: "profile.upgraded", Data: Object{
			"version": JournalVersion, "baseline": map[string]*DefinitionBasis{},
		}},
		{ID: ID(), Sequence: 2, At: stamp(), Action: "task.add", Target: taskID, Data: Object{"title": "legacy"}},
	}
	state := NewState()
	for _, event := range initial {
		if err := applyReplayEvent(state, event, ""); err != nil {
			t.Fatal(err)
		}
	}
	historical := walFrame{
		Meta:   walFrameMeta{Kind: "historical", PreviousRevision: 0, FinalRevision: 2, EventCount: len(initial)},
		Events: initial,
	}
	if _, err := appendPreparedFrame(resolution, historical, nil, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(resolution.WAL)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState("test", state, info.Size(), stamp())); err != nil {
		t.Fatal(err)
	}

	const expected = "v3-receipt-definition-signature"
	requestID := ID()
	fingerprint := "claim-fingerprint"
	claim := Event{
		ID: ID(), Sequence: 3, At: stamp(), RequestID: requestID, Action: "run.claimed", Target: taskID,
		Data: Object{"run_id": runID, "directory": "/tmp"},
	}
	receipt := newReceiptCoordination(requestID, fingerprint, "", Object{
		"item": Object{"definition_signature": expected},
		"run":  Object{"definition_signature": expected},
	}, "")
	ref := receiptReference(receipt)
	frame := walFrame{
		Meta: walFrameMeta{
			Kind: "mutation", PreviousRevision: 2, FinalRevision: 3,
			RequestID: requestID, Fingerprint: fingerprint, EventCount: 1, HasReceipt: true,
		},
		Events: []Event{claim}, Receipt: &ref,
	}
	if _, err := appendPreparedFrame(resolution, frame, &receipt, nil); err != nil {
		t.Fatal(err)
	}

	current, _, err := loadCurrentV3Cached(resolution)
	if err != nil {
		t.Fatal(err)
	}
	if current.Runs[runID].Signature != expected {
		t.Fatalf("current signature = %q", current.Runs[runID].Signature)
	}
	history, err := loadHistoryV3(resolution)
	if err != nil {
		t.Fatal(err)
	}
	if history.Runs[runID].Signature != expected {
		t.Fatalf("history signature = %q", history.Runs[runID].Signature)
	}
	if str(history.Events[2].Data, "definition_signature") != "" {
		t.Fatal("v3 receipt enrichment changed logical history")
	}
	atRevision, revisionErr := history.AtRevision(3)
	if revisionErr != nil || atRevision.Runs[runID].Signature != expected {
		t.Fatalf("history at revision signature = %q err=%v", atRevision.Runs[runID].Signature, revisionErr)
	}
}
