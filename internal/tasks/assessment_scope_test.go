package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestEditPreviewFromCurrentSnapshotMatchesFullHistory(t *testing.T) {
	store, workstream, _, _ := currentFixture(t)
	history, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.ReadCurrent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Events) >= len(history.Events) {
		t.Fatal("fixture does not distinguish current state from full history")
	}
	body := editBody(Object{"op": "workstream.update", "value": Object{"title": "Preview updated title"}})
	full, evaluationErr := EvaluateEdit(history, workstream, body, nil)
	if evaluationErr != nil {
		t.Fatal(evaluationErr)
	}
	preview, previewErr := store.Execute(context.Background(), Request{Action: "workstream.edited", Target: workstream, Body: body,
		Options: map[string]string{"if-revision": fmt.Sprint(current.Revision), "dry-run": "true"}})
	if previewErr != nil {
		t.Fatal(previewErr)
	}
	for _, key := range []string{"would_change", "changes", "effects", "impact", "issues", "next_actions", "created_refs", "created_items"} {
		want, _ := json.Marshal(full.Result[key])
		got, _ := json.Marshal(preview[key])
		if string(want) != string(got) {
			t.Fatalf("preview changed %s", key)
		}
	}
}

func TestScopedWorkstreamQueriesMatchFullAssessmentAcrossCompletionAndEdit(t *testing.T) {
	store, workstream, task, validation := currentFixture(t)
	call(t, store, "workstream.create", "", Object{"title": "Unrelated workstream"})
	check := func() {
		t.Helper()
		state, err := store.ReadCurrent(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		expected, _ := json.Marshal(state.View(state.Items[workstream]))
		rows, err := store.Query(context.Background(), Query{Command: "workstream list", Options: map[string]string{"state": "active"}})
		if err != nil || len(rows["items"].([]any)) != 1 {
			t.Fatal("filtered list changed scope", rows, err)
		}
		row, _ := json.Marshal(rows["items"].([]any)[0])
		if string(row) != string(expected) {
			t.Fatal("filtered list changed assessment")
		}
		for _, command := range []string{"workstream show", "workstream context"} {
			result, err := store.Query(context.Background(), Query{Command: command, Target: workstream})
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := json.Marshal(result["item"])
			if string(actual) != string(expected) {
				t.Fatalf("%s changed assessment", command)
			}
			if command == "workstream context" {
				for _, view := range result["tasks"].([]any) {
					item := state.Items[str(objectValue(view), "id")]
					want, _ := json.Marshal(state.View(item))
					got, _ := json.Marshal(view)
					if string(want) != string(got) {
						t.Fatal("context changed task assessment")
					}
				}
			}
		}
	}
	check()
	claim := call(t, store, "run.claimed", task, Object{})
	token := str(claim, "context")
	recordPass(t, store, validation, token)
	call(t, store, "task.completed", task, Object{"summary": "complete"}, "context", token)
	check()
	call(t, store, "workstream.edited", workstream, editBody(Object{"op": "task.update", "id": task, "value": Object{"description": "changed"}}))
	check()
}

func TestScopedAssessmentsPreservePrerequisitesMembersAndEvidence(t *testing.T) {
	state, _ := benchmarkActiveWorkstreamState(4 << 10)
	workstreams := state.List("workstream")
	target, prerequisite, unrelated := workstreams[0], workstreams[1], workstreams[2]
	target.Depends = []string{prerequisite.ID}
	prerequisite.State = "done"
	state.definition(prerequisite.ID).Completions = []CompletionBasis{{Revision: 1, Signature: "stale"}}
	tasks := state.List("task")
	for _, item := range tasks {
		if item.Workstream == target.ID {
			id := ID()
			state.Items[id] = &Item{ID: id, Kind: "validation", State: "open", Depends: []string{}, Props: Object{"task_id": item.ID, "method": "test", "required": true}}
			state.Tracking[id] = newDefinitionBasis(1)
			break
		}
	}
	full := state.Assessments()
	state.assessments = nil
	scoped := state.assessTargets([]string{target.ID})
	for id, assessment := range scoped {
		if !reflect.DeepEqual(assessment, full[id]) {
			t.Fatalf("assessment changed for %s", id)
		}
	}
	if _, ok := scoped[target.ID]; !ok {
		t.Fatal("missing selected workstream")
	}
	if _, ok := scoped[prerequisite.ID]; !ok {
		t.Fatal("missing prerequisite workstream")
	}
	if _, ok := scoped[unrelated.ID]; ok {
		t.Fatal("assessed unrelated workstream")
	}
	for _, item := range tasks {
		if item.Workstream == target.ID || item.Workstream == prerequisite.ID {
			if _, ok := scoped[item.ID]; !ok {
				t.Fatal("missing task in selected closure")
			}
		}
	}
	if state.assessments != nil {
		t.Fatal("scoped assessment replaced mutation-wide cache")
	}
}
