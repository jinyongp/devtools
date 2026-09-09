package tasks

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestArchiveCurrentScopeAndProof(t *testing.T) {
	s := fixture(t)
	id := itemID(call(t, s, "task.add", "", Object{"title": "Archive"}))
	claim := call(t, s, "run.claimed", id, Object{})
	call(t, s, "task.completed", id, Object{"summary": "done"}, "context", str(claim, "context"))
	ready := func() bool {
		b, e := os.ReadFile(s.path())
		if e != nil {
			t.Fatal(e)
		}
		return ArchiveReady(b, s.Profile, time.Now().Add(time.Hour))
	}
	if !ready() {
		t.Fatal("current completion not archivable")
	}
	call(t, s, "task.reopen", id, Object{"reason": "more work"})
	if ready() {
		t.Fatal("open work archived")
	}
	call(t, s, "task.cancel", id, Object{"reason": "withdrawn"})
	if !ready() {
		t.Fatal("cancellation blocks archive")
	}

	s, w, task, v := currentFixture(t)
	claim = call(t, s, "run.claimed", task, Object{})
	recordPass(t, s, v, str(claim, "context"))
	call(t, s, "task.completed", task, Object{"summary": "done"}, "context", str(claim, "context"))
	extra := itemID(call(t, s, "task.add", "", Object{"title": "Excluded unfinished", "workstream_id": w}))
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.remove", "id": extra}))
	call(t, s, "workstream.close", w, Object{})
	if !ready() {
		t.Fatal("closed workstream not archivable")
	}
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "changed"}}))
	if ready() {
		t.Fatal("stale done archived")
	}
}

func TestRestorePreservesV2AndDropsExecutionAuthority(t *testing.T) {
	s, w, task, _ := currentFixture(t)
	claim := call(t, s, "run.claimed", task, Object{})
	call(t, s, "workstream.edited", w, editBody(Object{"op": "task.remove", "id": task}))
	data, e := os.ReadFile(s.path())
	if e != nil {
		t.Fatal(e)
	}
	if ArchiveReady(data, s.Profile, time.Now().Add(time.Hour)) {
		t.Fatal("removed running archived")
	}
	restored, e := RestoreSnapshot(data, s.Profile, "restored")
	if e != nil {
		t.Fatal(e)
	}
	var j Journal
	if e = json.Unmarshal(restored, &j); e != nil {
		t.Fatal(e)
	}
	state, err := replayJournal(&j)
	if err != nil {
		t.Fatal(err)
	}
	if j.Version != 2 || len(j.Contexts) != 0 || len(j.Receipts) != 0 || state.Current(task) != nil || state.Included(state.Items[task]) {
		t.Fatal("restore changed scope or retained authority")
	}
	if state.Runs[runID(claim)].State != "released" {
		t.Fatal("source run not released")
	}
	before, _ := s.Read()
	if hash(before.Tracking) != hash(state.Tracking) {
		t.Fatal("restoration changed definition history")
	}
}
