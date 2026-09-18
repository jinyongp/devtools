package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func contextBasis(t *testing.T, store Store, task string) Object {
	t.Helper()
	view, err := store.Query(Query{Command: "context", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	basis, ok := view["context_basis"].(Object)
	if !ok {
		t.Fatalf("context basis = %#v", view["context_basis"])
	}
	return basis
}

func compactCheckpoint(t *testing.T, store Store, claim Object, basis Object, summary string) Object {
	t.Helper()
	return call(t, store, "run.checkpointed", runID(claim), Object{
		"summary":                summary,
		"decisions":              []string{"preserve canonical history"},
		"remaining":              []string{"continue"},
		"next_action":            "resume from deterministic context",
		"blockers":               []string{},
		"validation_record_ids":  []string{},
		"compaction_fingerprint": str(basis, "fingerprint"),
		"compaction_through":     fmt.Sprint(num(basis, "through_sequence")),
	}, "context", str(claim, "context"))
}

func TestTaskContextRecoversWithoutCompaction(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "Recover"}))
	claim := call(t, store, "run.claimed", task, Object{})

	view, err := store.Query(Query{Command: "context", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	basis := view["context_basis"].(Object)
	if str(basis, "fingerprint") == "" || num(basis, "through_sequence") == 0 || num(basis, "event_count") < 2 {
		t.Fatalf("basis = %#v", basis)
	}
	if view["compaction"] != nil {
		t.Fatalf("unexpected compaction = %#v", view["compaction"])
	}
	item := objectValue(view["item"])
	if str(item, "id") != task || item["running"] != true {
		t.Fatalf("current item = %#v", item)
	}
	delta := view["delta"].([]Event)
	if len(delta) < 2 || delta[len(delta)-1].Action != "run.claimed" {
		t.Fatalf("no-summary delta = %#v", delta)
	}
	if str(claim, "context") == "" {
		t.Fatal("claim context missing from fixture")
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), str(claim, "context")) {
		t.Fatal("claim context leaked through task context")
	}
}

func TestCompactionBasisIgnoresUnrelatedProfileChanges(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "Scoped"}))
	claim := call(t, store, "run.claimed", task, Object{})
	basis := contextBasis(t, store, task)

	other := itemID(call(t, store, "task.add", "", Object{"title": "Unrelated"}))
	call(t, store, "task.update", other, Object{"description": "does not affect scoped context"})
	compacted := compactCheckpoint(t, store, claim, basis, "Scoped summary")
	if compacted["changed"] != true {
		t.Fatalf("compaction checkpoint = %#v", compacted)
	}

	view, err := store.Query(Query{Command: "context", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	compaction := view["compaction"].(Object)
	if str(compaction, "summary") != "Scoped summary" || str(compaction, "basis_fingerprint") != str(basis, "fingerprint") {
		t.Fatalf("compaction = %#v", compaction)
	}
	if delta := view["delta"].([]Event); len(delta) != 0 {
		t.Fatalf("unrelated events entered task delta: %#v", delta)
	}
}

func TestCompactionRejectsRelatedChangeAfterPrepare(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "Scoped"}))
	claim := call(t, store, "run.claimed", task, Object{})
	basis := contextBasis(t, store, task)
	call(t, store, "run.checkpointed", runID(claim), Object{"summary": "related progress"}, "context", str(claim, "context"))

	before, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	_, failure := store.Execute(context.Background(), Request{
		Action: "run.checkpointed",
		Target: runID(claim),
		Body: Object{
			"summary":                "stale summary",
			"compaction_fingerprint": str(basis, "fingerprint"),
			"compaction_through":     fmt.Sprint(num(basis, "through_sequence")),
		},
		Options: map[string]string{"request-id": ID(), "context": str(claim, "context")},
	})
	if failure == nil || failure.Code != "revision_conflict" {
		t.Fatalf("stale compaction failure = %+v", failure)
	}
	after, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || len(after.Events) != len(before.Events) {
		t.Fatal("stale compaction appended history")
	}
}

func TestTaskContextReturnsCompactionAndPostBasisDelta(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "Delta"}))
	claim := call(t, store, "run.claimed", task, Object{})
	basis := contextBasis(t, store, task)
	compactCheckpoint(t, store, claim, basis, "Compressed explanation")
	call(t, store, "run.checkpointed", runID(claim), Object{"summary": "after compaction"}, "context", str(claim, "context"))

	view, err := store.Query(Query{Command: "context", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	compaction := view["compaction"].(Object)
	if str(compaction, "summary") != "Compressed explanation" || str(compaction, "run_id") != runID(claim) {
		t.Fatalf("compaction = %#v", compaction)
	}
	delta := view["delta"].([]Event)
	if len(delta) != 1 || delta[0].Action != "run.checkpointed" || str(delta[0].Data, "summary") != "after compaction" {
		t.Fatalf("delta = %#v", delta)
	}
	status := view["delta_status"].(Object)
	if status["truncated"] != false || num(status, "omitted_count") != 0 {
		t.Fatalf("delta status = %#v", status)
	}
}

func TestTaskContextDeltaReportsExplicitTruncation(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "Many events"}))
	claim := call(t, store, "run.claimed", task, Object{})
	basis := contextBasis(t, store, task)
	compactCheckpoint(t, store, claim, basis, "Baseline")
	for index := 0; index < contextDeltaLimit+5; index++ {
		call(t, store, "run.checkpointed", runID(claim), Object{"summary": fmt.Sprintf("event-%02d", index)}, "context", str(claim, "context"))
	}
	view, err := store.Query(Query{Command: "context", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	delta := view["delta"].([]Event)
	status := view["delta_status"].(Object)
	if len(delta) != contextDeltaLimit || status["truncated"] != true || num(status, "omitted_count") != 5 {
		t.Fatalf("delta=%d status=%#v", len(delta), status)
	}
	if num(status, "omitted_through_sequence") == 0 || num(status, "returned_from_sequence") == 0 || num(status, "returned_through_sequence") == 0 {
		t.Fatalf("truncation range = %#v", status)
	}
}

func TestCompactionMetadataSurvivesExportAndReplay(t *testing.T) {
	store := fixture(t)
	task := itemID(call(t, store, "task.add", "", Object{"title": "Portable"}))
	claim := call(t, store, "run.claimed", task, Object{})
	basis := contextBasis(t, store, task)
	compactCheckpoint(t, store, claim, basis, "Portable summary")

	exported, err := store.Query(Query{Command: "export", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(exported)
	if !strings.Contains(string(raw), "compaction_fingerprint") || !strings.Contains(string(raw), "Portable summary") {
		t.Fatalf("export omitted compaction metadata: %s", raw)
	}
	reloaded, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != JournalVersion {
		t.Fatalf("journal version changed: %d", reloaded.Version)
	}
	view, err := store.Query(Query{Command: "context", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	if compaction := view["compaction"].(Object); str(compaction, "summary") != "Portable summary" {
		t.Fatalf("replayed compaction = %#v", compaction)
	}
}

func TestCompactionBasisTracksPrerequisiteChanges(t *testing.T) {
	store, workstream, task, _ := currentFixture(t)
	edit := call(t, store, "workstream.edited", workstream, editBody(
		Object{"op": "task.add", "ref": "prereq", "value": Object{"title": "Prerequisite", "acceptance_keys": []string{"A"}}},
		Object{"op": "validation.add", "ref": "prereq-check", "value": Object{"title": "Prereq check", "method": "test", "task_id": "@prereq"}},
		Object{"op": "task.depends.set", "id": task, "depends_on": []string{"@prereq"}},
	))
	refs := objectValue(edit["created_refs"])
	prereq := str(refs, "prereq")
	prereqValidation := str(refs, "prereq-check")
	prereqClaim := call(t, store, "run.claimed", prereq, Object{})
	recordPass(t, store, prereqValidation, str(prereqClaim, "context"))
	call(t, store, "task.completed", prereq, Object{"summary": "prerequisite done"}, "context", str(prereqClaim, "context"))

	claim := call(t, store, "run.claimed", task, Object{})
	basis := contextBasis(t, store, task)
	call(t, store, "workstream.edited", workstream, editBody(
		Object{"op": "task.update", "id": prereq, "value": Object{"description": "changed prerequisite definition"}},
	))

	before, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	_, failure := store.Execute(context.Background(), Request{
		Action: "run.checkpointed",
		Target: runID(claim),
		Body: Object{
			"summary":                "stale after prerequisite change",
			"compaction_fingerprint": str(basis, "fingerprint"),
			"compaction_through":     fmt.Sprint(num(basis, "through_sequence")),
		},
		Options: map[string]string{"request-id": ID(), "context": str(claim, "context")},
	})
	if failure == nil || failure.Code != "revision_conflict" {
		t.Fatalf("prerequisite change did not invalidate compaction: %+v", failure)
	}
	after, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision {
		t.Fatal("stale dependency compaction appended history")
	}
	view, err := store.Query(Query{Command: "context", Target: task})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range view["delta"].([]Event) {
		if event.Action == "workstream.edited" {
			for _, patch := range objects(event.Data, "patches") {
				if str(patch, "id") == prereq {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("prerequisite change missing from task context delta")
	}
}
