package tasks

import (
	"reflect"
	"sort"
	"testing"
)

func TestCompletionFiltersLifecycleAndCurrentRuns(t *testing.T) {
	state := NewState()
	for _, name := range []string{"open", "held", "done", "canceled", "running"} {
		item := &Item{ID: name, Kind: "task", State: name, Props: Object{}}
		if name == "held" || name == "running" {
			item.State = "open"
		}
		if name == "held" {
			item.Props["hold"] = "waiting"
		}
		state.Items[name] = item
	}
	state.Runs["live"] = &Run{ID: "live", TaskID: "running", State: "running"}
	state.Runs["ended"] = &Run{ID: "ended", TaskID: "done", State: "completed"}
	for _, test := range []struct {
		kind, action string
		want         []string
	}{
		{"task", "task.reopen", []string{"canceled", "done"}},
		{"task", "task.unhold", []string{"held"}},
		{"task", "run.revoked", []string{"running"}},
		{"run", "run.checkpointed", []string{"live"}},
		{"run", "", []string{"ended", "live"}},
		{"task", "", []string{"canceled", "done", "held", "open", "running"}},
	} {
		got := state.completionIDs(test.kind, "", CompletionFilter{Action: test.action})
		sort.Strings(got)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s: got %v want %v", test.action, got, test.want)
		}
	}
}

func TestCompletionDependencyCandidatesUseGraphContract(t *testing.T) {
	state := NewState()
	for _, name := range []string{"self", "before", "after", "other"} {
		state.Items[name] = &Item{ID: name, Kind: "task", State: "open", Workstream: "stream", Props: Object{}}
	}
	state.Items["other"].Workstream = "elsewhere"
	state.Items["after"].Depends = []string{"self"}
	got := state.completionIDs("task", "", CompletionFilter{Action: "task.depends", Field: "depends-on", Target: "self"})
	if !reflect.DeepEqual(got, []string{"before"}) {
		t.Fatal(got)
	}
}

func TestCompletionScopesPlanMembersAndKeepsCompletedDefinitionEdits(t *testing.T) {
	state := NewState()
	state.Version = JournalVersion
	state.Items["stream"] = &Item{ID: "stream", Kind: "workstream", State: "done", Props: Object{}}
	state.Items["task"] = &Item{ID: "task", Kind: "task", State: "done", Workstream: "stream", Props: Object{}}
	state.Items["other"] = &Item{ID: "other", Kind: "task", State: "open", Workstream: "elsewhere", Props: Object{}}
	state.Items["validation"] = &Item{ID: "validation", Kind: "validation", State: "open", Props: Object{"task_id": "task"}}
	for _, kind := range []string{"task", "validation"} {
		got := state.completionIDs(kind, "", CompletionFilter{Action: "plan.set", Field: kind + "-ids", Target: "stream"})
		if !reflect.DeepEqual(got, []string{kind}) {
			t.Fatal(kind, got)
		}
	}
	got := state.completionIDs("workstream", "", CompletionFilter{Action: "task.add", Field: "workstream"})
	if !reflect.DeepEqual(got, []string{"stream"}) {
		t.Fatal("completed workstreams still accept definition additions", got)
	}
}
