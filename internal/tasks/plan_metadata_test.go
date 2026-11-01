package tasks

import (
	"context"
	"testing"
)

func TestPlanQueryNamesReferencesWithoutChangingDocument(t *testing.T) {
	store, workstream, _, validation := currentFixture(t)
	before, err := store.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	document := hash(before.Items[workstream].Props["plan"])
	result, err := store.Query(context.Background(), Query{Command: "workstream plan show", Target: workstream})
	if err != nil || result["title"] != "Current" || hash(result["item"]) != document {
		t.Fatal("query changed reusable document body", result, err)
	}
	refs, _ := result["validations"].([]Object)
	if len(refs) != 1 || refs[0]["id"] != validation || refs[0]["title"] != "Check" {
		t.Fatal("reference titles lost", result["validations"])
	}
	after, err := store.Read(context.Background())
	if err != nil || hash(after.Items[workstream].Props["plan"]) != document {
		t.Fatal("read mutated stored document", err)
	}
}
