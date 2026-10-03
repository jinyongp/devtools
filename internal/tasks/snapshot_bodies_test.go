package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactSnapshotPreservesDocumentsHistoryAndSignatures(t *testing.T) {
	s := fixture(t)
	result, apiErr := s.Execute(context.Background(), Request{Action: "workstream.create", Body: Object{"title": "Documents"}, Options: map[string]string{"request-id": ID()}})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	id := result["item"].(Object)["id"].(string)
	body := strings.Repeat("Unicode 한글 <>&\n", 512)
	for _, action := range []string{"spec.set", "plan.set"} {
		data := Object{"body": body}
		if action == "spec.set" {
			data["requirements"], data["acceptance"] = []Object{}, []Object{}
		} else {
			data["task_ids"], data["validation_ids"] = []string{}, []string{}
		}
		call(t, s, action, id, data)
	}
	before, readErr := s.ReadCurrent(context.Background())
	if readErr != nil {
		t.Fatal(readErr)
	}
	snapshot := snapshotFromState(s.Profile, before, 0, "")
	if snapshot.FormatVersion != compactSnapshotVersion || len(snapshot.DocumentBodies) != 1 {
		t.Fatal("document bodies were not interned")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	bodyJSON, _ := json.Marshal(body)
	if strings.Count(string(encoded), string(bodyJSON)) != 1 {
		t.Fatal("snapshot still duplicates document bodies")
	}
	path := filepath.Join(privateTempDir(t), "snapshot.json")
	if err := writeMaterializedSnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	stored, after, err := readMaterializedSnapshot(path, s.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if hash(before.Items) != hash(after.Items) || hash(before.HistoryEvents) != hash(after.HistoryEvents) || hash(before.HistoryRefs) != hash(after.HistoryRefs) || hash(before.Assessments()) != hash(after.Assessments()) {
		t.Fatal("snapshot changed projected documents, history or signatures")
	}
	if stored.Checksum != materializedStateChecksum(stored) {
		t.Fatal("projection expansion mutated encoded snapshot")
	}
	// A version 3 snapshot remains readable with its original checksum contract.
	legacy := stored
	legacy.Items, legacy.HistoryEvents = after.Items, after.HistoryEvents
	legacy.DocumentBodies = nil
	legacy.FormatVersion = taskStorageVersion
	legacy.Checksum = materializedStateChecksum(legacy)
	if _, err := legacy.state(s.Profile); err != nil {
		t.Fatal("legacy snapshot rejected", err)
	}
	query := Query{Command: "workstream context", Target: id}
	expected, queryErr := s.Query(context.Background(), query)
	if queryErr != nil {
		t.Fatal(queryErr)
	}
	resolution, ok, resolveErr := resolveV3(s.Directory, s.Profile)
	if resolveErr != nil || !ok {
		t.Fatal(resolveErr)
	}
	wal, statErr := os.Stat(resolution.WAL)
	if statErr != nil {
		t.Fatal(statErr)
	}
	stored.WALOffset = wal.Size()
	stored.Checksum = materializedStateChecksum(stored)
	if err := writeMaterializedSnapshot(resolution.Snapshot, stored); err != nil {
		t.Fatal(err)
	}
	actual, queryErr := s.Query(context.Background(), query)
	if queryErr != nil || hash(actual) != hash(expected) {
		t.Fatal("compact snapshot changed public context", queryErr)
	}

	stored.DocumentBodies[0] = "tampered"
	if _, err := stored.state(s.Profile); err == nil {
		t.Fatal("trusted corrupted document table")
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, stored); err != nil {
		t.Fatal(err)
	}
	actual, queryErr = s.Query(context.Background(), query)
	if queryErr != nil || hash(actual) != hash(expected) {
		t.Fatal("corrupt document table did not recover from WAL", queryErr)
	}
}

func TestCompactSnapshotRejectsInvalidReferences(t *testing.T) {
	for _, ref := range []Object{{"snapshot_body": -1}, {"snapshot_body": 1}, {"snapshot_body": 0.5}, {"snapshot_body": "0"}, {"snapshot_body": 0, "extra": true}, {"unexpected": 0}} {
		m := materializedState{FormatVersion: compactSnapshotVersion, Profile: "test", Version: JournalVersion, Revision: 1,
			Items:      map[string]*Item{"w": {ID: "w", Kind: "workstream", Props: Object{"spec": Object{"body": ref}}}},
			ItemOrders: map[string]int{"w": 1}, Runs: map[string]*Run{}, Tracking: map[string]*DefinitionBasis{}, DocumentBodies: []string{"body"}}
		m.Checksum = materializedStateChecksum(m)
		if _, err := m.state("test"); err == nil {
			t.Fatal("accepted invalid document reference", ref)
		}
	}
}
