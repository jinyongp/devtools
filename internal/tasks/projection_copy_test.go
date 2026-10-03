package tasks

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestJSONCopyPreservesEncodingAndIsolatesContainers(t *testing.T) {
	value := Object{"body": strings.Repeat("문서<&>\n", 1000), "number": 42,
		"nested":      Object{"items": []Object{{"keys": []string{"one", "two"}}}},
		"null_object": Object(nil), "null_array": []string(nil), "empty": []any{}}
	raw, _ := json.Marshal(value)
	var expected Object
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	copied := copyObject(value)
	if !reflect.DeepEqual(copied, expected) {
		t.Fatalf("copy differs from JSON: %#v", copied)
	}
	nested := objectValue(copied["nested"])["items"].([]any)[0].(map[string]any)
	nested["keys"].([]any)[0] = "changed"
	if objectValue(value["nested"])["items"].([]Object)[0]["keys"].([]string)[0] != "one" {
		t.Fatal("copy shares nested arrays")
	}
}

func TestProjectionCopyPreservesStateAndIsolatesHistoryAndDefinitions(t *testing.T) {
	state, _ := benchmarkActiveWorkstreamState(16 << 10)
	item := state.List("workstream")[0]
	event := Event{Sequence: state.Revision + 1, Target: item.ID, Action: "plan.set", Data: Object{"body": "updated"}}
	state.Apply(event)
	basis := state.definition(item.ID)
	basis.Completions = []CompletionBasis{{Result: Object{"keys": []string{"original"}}}}
	state.Runs["run"] = &Run{ID: "run", TaskID: state.List("task")[0].ID, State: "completed"}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	copied := state.clone()
	actual, err := json.Marshal(copied)
	if err != nil || string(raw) != string(actual) {
		t.Fatal("projection encoding changed", err)
	}
	copied.Items[item.ID].Props["spec"].(map[string]any)["body"] = "changed"
	copied.definition(item.ID).BodyEpochs["spec"] = 9999
	copied.definition(item.ID).Completions[0].Result["keys"].([]any)[0] = "changed"
	copied.Runs["run"].State = "running"
	copied.Events[len(copied.Events)-1].Data["body"] = "changed"
	copied.HistoryEvents[event.Sequence].Data["body"] = "changed"
	copied.HistoryRefs[item.ID][0] = 9999
	after, _ := json.Marshal(state)
	if string(raw) != string(after) {
		t.Fatal("copy changed original state")
	}
	if copied.Items[item.ID].Order != item.Order {
		t.Fatal("item order changed")
	}
}

func TestStreamHashAndResumedDefinitionHashPreserveSignatures(t *testing.T) {
	for _, body := range []string{"", "plain", "문서<&>\u2028\u2029\n\"\\", strings.Repeat("x", 256<<10)} {
		common := Object{"spec": body, "plan": body, "epochs": map[string]int{"spec": 42, "plan": 21}}
		resume := commonDefinitionHasher(common)
		for _, id := range []string{"one", "two"} {
			definition := Object{"common": common, "self": Object{"id": id}, "criteria": []Object{},
				"dependencies": []Object{}, "epoch": 42, "included": true, "validations": []Object{}, "workstream_dependencies": []Object{}}
			expected := hash(definition)
			if streamJSONHash(definition) != expected || resume(definition) != expected {
				t.Fatal("definition signature changed")
			}
		}
	}
	for _, value := range []any{nil, []any{}, Object{"null": nil, "escaped": "<&>"}, Run{ID: "run", Signature: "signature"}} {
		if streamJSONHash(value) != hash(value) {
			t.Fatal("canonical JSON checksum changed")
		}
	}
}

func TestChangedItemIDsRetainsCanonicalJSONComparison(t *testing.T) {
	before := NewState()
	id := ID()
	before.Items[id] = &Item{ID: id, Kind: "task", State: "open", Props: Object{"number": 42}, Order: 1}
	before.Tracking[id] = newDefinitionBasis(1)
	after := before.clone()
	// JSON normalization changes dynamic numeric types, and Order is private
	// projection metadata. Neither alone represents a changed public item.
	after.Items[id].Order = 2
	if len(changedItemIDs(before, after)) != 0 {
		t.Fatal("reported a non-JSON change")
	}
	after.Items[id].Title = "changed"
	if !reflect.DeepEqual(changedItemIDs(before, after), []string{id}) {
		t.Fatal("missed item mutation")
	}
	after.Items[id].Title = before.Items[id].Title
	before.Items[id].Props["number"] = float64(0)
	after.Items[id].Props["number"] = math.Copysign(0, -1)
	if !reflect.DeepEqual(changedItemIDs(before, after), []string{id}) {
		t.Fatal("missed JSON signed-zero change")
	}
}
