package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// Build genuine v1 events through the legacy reducer, without using the v2
// Store writer. This keeps migration tests independent of its upgrade path.
func legacyFixture(t *testing.T) (Store, *State, string, string, string) {
	t.Helper()
	store := fixture(t)
	s := NewState()
	contexts := map[string]string{}
	apply := func(action, id string, body Object, token string) Object {
		r := Request{Action: action, Target: id, Body: body, Options: map[string]string{
			"if-revision": fmt.Sprint(s.Revision), "context": token, "dir": "/legacy"}}
		// Public JSON input normalizes maps and arrays before preparation.
		raw, _ := json.Marshal(body)
		r.Body, _ = Decode(string(raw))
		events, result, credential, e := s.prepare(r, contexts)
		if e != nil {
			t.Fatal(e)
		}
		for _, event := range events {
			event.ID, event.RequestID, event.Sequence, event.At = ID(), ID(), s.Revision+1, stamp()
			raw, _ := json.Marshal(event)
			_ = json.Unmarshal(raw, &event)
			s.Apply(event)
			for _, key := range []string{"basis_id", "record_id", "run_id"} {
				if value := str(event.Data, key); value != "" {
					result[key] = value
				}
			}
		}
		if credential != "" {
			contexts[hash(credential)] = s.Current(str(result, "target_id")).ID
			result["context"] = credential
		}
		return result
	}
	id := str(apply("task.add", "", Object{"title": "Legacy completed"}, ""), "target_id")
	val := str(apply("validation.add", "", Object{"title": "Check", "method": "test", "task_id": id}, ""), "target_id")
	claim := apply("run.claimed", id, Object{}, "")
	basis := apply("validation.basis", val, Object{"code": []any{}}, "")
	apply("validation.record", val, Object{"basis_id": basis["basis_id"], "result": "pass", "summary": "ok", "evidence": []any{}}, str(claim, "context"))
	apply("task.completed", id, Object{"summary": "done"}, str(claim, "context"))
	active := str(apply("task.add", "", Object{"title": "Legacy running"}, ""), "target_id")
	running := apply("run.claimed", active, Object{}, "")
	if err := PrivateDir(store.Directory); err != nil {
		t.Fatal(err)
	}
	j := Journal{Version: 1, Profile: store.Profile, Events: s.Events, Contexts: contexts, Receipts: map[string]Receipt{}}
	if err := WritePrivate(store.path(), j); err != nil {
		t.Fatal(err)
	}
	return store, s, id, val, str(running, "context")
}

func TestUpgradePreservesLegacyEvidenceAndRuns(t *testing.T) {
	store, before, done, val, token := legacyFixture(t)
	rawBefore, _ := os.ReadFile(store.path())
	read, e := store.Read()
	if e != nil || read.Version != 1 {
		t.Fatal(read, e)
	}
	rawAfter, _ := os.ReadFile(store.path())
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("read rewrote legacy journal")
	}
	var run *Run
	for _, r := range before.Runs {
		if r.State == "running" {
			run = r
		}
	}
	result := call(t, store, "run.checkpointed", run.ID, Object{"summary": "continue"}, "context", token)
	after, e := store.Read()
	if e != nil || after.Version != 2 {
		t.Fatal(e)
	}
	if after.Items[done].State != "done" || len(after.definition(done).Completions) != 1 {
		t.Fatal("completion lost")
	}
	if after.Runs[run.ID].Definition != run.Definition || after.Runs[run.ID].State != "running" {
		t.Fatal("run changed")
	}
	if hash(after.Items[val].Props) != hash(before.Items[val].Props) {
		t.Fatal("evidence rewritten")
	}
	basisID := str(before.Items[val].Props, "current_basis")
	if after.definition(val).LegacyBases[basisID] == "" {
		t.Fatal("valid evidence not carried")
	}
	if _, e := after.AtRevision(before.Revision + 1); e == nil || e.Code != "revision_not_committed" {
		t.Fatal("partial upgrade exposed", e)
	}
	historical, e := after.AtRevision(before.Revision)
	if e != nil || historical.Version != 1 || hash(historical.Items) != hash(before.Items) {
		t.Fatal("legacy replay drift", e)
	}
	current, e := after.AtRevision(num(result, "revision"))
	if e != nil || hash(current.Tracking) != hash(after.Tracking) {
		t.Fatal("v2 replay drift", e)
	}
}

func TestNoOpDoesNotUpgrade(t *testing.T) {
	store, _, _, _, _ := legacyFixture(t)
	o := call(t, store, "run.claimed", "", Object{})
	if o["changed"] != false || o["claimed"] != false {
		t.Fatal(o)
	}
	s, e := store.Read()
	if e != nil || s.Version != 1 {
		t.Fatal("no-op upgraded", e)
	}
}

func TestReplayRejectsUnknownEventsAndVersions(t *testing.T) {
	store := fixture(t)
	call(t, store, "task.add", "", Object{"title": "safe"})
	var original Journal
	if e := ReadPrivate(store.path(), &original); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"version", "action", "sequence", "missing upgrade"} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var j Journal
			_ = json.Unmarshal(raw, &j)
			switch name {
			case "version":
				j.Version = 999
			case "action":
				j.Events[1].Action = "task.unknown"
			case "sequence":
				j.Events[1].Sequence = 99
			case "missing upgrade":
				j.Version = 1
			}
			if err := WritePrivate(store.path(), j); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(store.path())
			_, e := store.Execute(context.Background(), Request{Action: "task.add", Body: Object{"title": "unsafe"}, Options: map[string]string{"request-id": ID()}})
			if e == nil {
				t.Fatal("corrupt journal accepted")
			}
			after, _ := os.ReadFile(store.path())
			if string(before) != string(after) {
				t.Fatal("corrupt journal overwritten")
			}
		})
	}
}

func TestUpgradeDoesNotAdoptStaleLegacyBasis(t *testing.T) {
	store, s, _, val, _ := legacyFixture(t)
	j := Journal{}
	if e := ReadPrivate(store.path(), &j); e != nil {
		t.Fatal(e)
	}
	// A stale fingerprint can exist in archived evidence; migration must not
	// turn it into a valid current record just because it was once a pass.
	for n := range j.Events {
		if j.Events[n].Action == "validation.basis" {
			j.Events[n].Data["fingerprint"] = "old-definition"
		}
	}
	if e := WritePrivate(store.path(), j); e != nil {
		t.Fatal(e)
	}
	call(t, store, "task.add", "", Object{"title": "unrelated"})
	after, e := store.Read()
	if e != nil || len(after.definition(val).LegacyBases) != 0 {
		t.Fatal("stale basis adopted", e)
	}
	if after.Items[val].Revision != s.Items[val].Revision {
		t.Fatal("definition rewritten")
	}
}

func TestCloneDoesNotShareProjection(t *testing.T) {
	store := fixture(t)
	id := itemID(call(t, store, "task.add", "", Object{"title": "original"}))
	s, _ := store.Read()
	clone := s.clone()
	clone.Items[id].Props["title"] = "changed"
	clone.definition(id).Removed = true
	if s.Items[id].Props["title"] != "original" || s.definition(id).Removed {
		t.Fatal("clone shares state")
	}
	if clone.Items[id].Order != s.Items[id].Order {
		t.Fatal("clone loses selection order")
	}
}
