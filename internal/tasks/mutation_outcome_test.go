package tasks

import (
	"context"
	"fmt"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestMutationOutcomeAndNoChangeFailure(t *testing.T) {
	s := fixture(t)
	created := call(t, s, "task.add", "", Object{"title": "Work"})
	id := itemID(created)
	if num(created, "previous_revision") != 0 || num(created, "affected_count") != 1 || !contains(arr(created, "affected_ids"), id) {
		t.Fatal("creation outcome is not verifiable", created)
	}
	state, _ := s.Read()
	revision := state.Revision
	requestID := ID()
	request := Request{Action: "task.update", Target: id, Body: Object{"title": "Work"}, Options: map[string]string{"request-id": requestID, "if-revision": fmt.Sprint(revision)}}
	if _, e := s.Execute(context.Background(), request); e == nil || e.Code != "no_change" || num(Object(e.Details), "affected_count") != 0 || len(arr(Object(e.Details), "affected_ids")) != 0 {
		t.Fatal("identical update did not fail as no_change", e)
	}
	after, _ := s.Read()
	if after.Revision != revision || len(after.Events) != len(state.Events) {
		t.Fatal("no_change mutated the journal")
	}
	request.Body = Object{"description": "Changed"}
	changed, e := s.Execute(context.Background(), request)
	if e != nil {
		t.Fatal("failed no_change wrote a receipt", e)
	}
	if num(changed, "previous_revision") != revision || num(changed, "revision") <= revision || num(changed, "current_revision") != num(changed, "revision") || num(changed, "affected_count") != 1 || !contains(arr(changed, "affected_ids"), id) {
		t.Fatal("changed update outcome is incomplete", changed)
	}
	call(t, s, "task.add", "", Object{"title": "Advance profile"})
	latest, _ := s.Read()
	replayed, e := s.Execute(context.Background(), request)
	if e != nil || replayed["replayed"] != true || num(replayed, "previous_revision") != num(changed, "previous_revision") || num(replayed, "revision") != num(changed, "revision") || num(replayed, "current_revision") != latest.Revision {
		t.Fatal("replayed mutation lost revision boundaries", replayed, e)
	}

	claim := call(t, s, "run.claimed", id, Object{})
	beforeCheckpoint, _ := s.Read()
	definitionRevision := beforeCheckpoint.Items[id].Revision
	checkpoint := call(t, s, "run.checkpointed", runID(claim), Object{"summary": "Saved"}, "context", str(claim, "context"))
	afterCheckpoint, _ := s.Read()
	if num(checkpoint, "previous_revision") != beforeCheckpoint.Revision || num(checkpoint, "revision") != afterCheckpoint.Revision || num(checkpoint, "affected_count") != 1 || !contains(arr(checkpoint, "affected_ids"), id) {
		t.Fatal("checkpoint outcome is incomplete", checkpoint)
	}
	if afterCheckpoint.Items[id].Revision != definitionRevision {
		t.Fatal("checkpoint changed task definition revision")
	}
}

func TestTaskUpdateContextGuardAndContextReasons(t *testing.T) {
	s, _, task, _ := currentFixture(t)
	first := call(t, s, "run.claimed", task, Object{})
	other := itemID(call(t, s, "task.add", "", Object{"title": "Other"}))
	second := call(t, s, "run.claimed", other, Object{})

	update := func(token, title, requestID string) (Object, *protocol.Error) {
		state, _ := s.Read()
		return s.Execute(context.Background(), Request{Action: "task.update", Target: task, Body: Object{"title": title}, Options: map[string]string{"request-id": requestID, "if-revision": fmt.Sprint(state.Revision), "context": token}})
	}
	assertReason := func(token, reason string) {
		t.Helper()
		_, e := update(token, "Rejected "+reason, ID())
		if e == nil || e.Code != "context_invalid" || e.Details["context_reason"] != reason {
			t.Fatalf("context reason %s: %+v", reason, e)
		}
	}
	assertReason("unknown-context", "unknown")
	assertReason(str(second, "context"), "target_mismatch")

	requestID := ID()
	changed, e := update(str(first, "context"), "Guarded update", requestID)
	if e != nil || num(changed, "affected_count") < 1 || !contains(arr(changed, "affected_ids"), task) {
		t.Fatal("matching context did not guard a successful update", changed, e)
	}
	if _, e = update(str(first, "context"), "Guarded update", ID()); e == nil || e.Code != "no_change" {
		t.Fatal("guarded no-op did not fail", e)
	}
	call(t, s, "run.released", runID(first), Object{}, "context", str(first, "context"))
	assertReason(str(first, "context"), "inactive")

	_, e = s.Execute(context.Background(), Request{Action: "run.checkpointed", Target: runID(second), Body: Object{"summary": "missing"}, Options: map[string]string{"request-id": ID()}})
	if e == nil || e.Code != "context_invalid" || e.Details["context_reason"] != "missing" {
		t.Fatal("missing checkpoint context is not specific", e)
	}
}

func TestSemanticNoChangeMatrixAndAuditException(t *testing.T) {
	assertNoChange := func(t *testing.T, s Store, action, target string, body Object, options map[string]string) {
		t.Helper()
		before, _ := s.Read()
		if options == nil {
			options = map[string]string{}
		}
		options["request-id"] = ID()
		if Find(action).Revision {
			options["if-revision"] = fmt.Sprint(before.Revision)
		}
		_, e := s.Execute(context.Background(), Request{Action: action, Target: target, Body: body, Options: options})
		if e == nil || e.Code != "no_change" {
			t.Fatalf("%s did not reject a semantic no-op: %+v", action, e)
		}
		after, _ := s.Read()
		if after.Revision != before.Revision || len(after.Events) != len(before.Events) {
			t.Fatalf("%s recorded a semantic no-op", action)
		}
	}

	s := fixture(t)
	independent := itemID(call(t, s, "task.add", "", Object{"title": "Independent"}))
	assertNoChange(t, s, "task.depends", independent, Object{"depends_on": []string{}}, nil)
	assertNoChange(t, s, "task.detach", independent, Object{}, nil)
	workstream := itemID(call(t, s, "workstream.create", "", Object{"title": "Draft"}))
	member := itemID(call(t, s, "task.add", "", Object{"title": "Member", "workstream_id": workstream}))
	assertNoChange(t, s, "task.attach", member, Object{"workstream_id": workstream}, nil)

	validationStore, validationWorkstream, task, validation := currentFixture(t)
	claim := call(t, validationStore, "run.claimed", task, Object{})
	token := str(claim, "context")
	call(t, validationStore, "validation.basis", validation, Object{"code": []any{}}, "context", token)
	call(t, validationStore, "validation.waive", validation, Object{"reason": "Accepted"}, "context", token)
	assertNoChange(t, validationStore, "validation.waive", validation, Object{"reason": "Accepted"}, map[string]string{"context": token})
	call(t, validationStore, "validation.unwaive", validation, Object{"reason": "Withdrawn"}, "context", token)
	assertNoChange(t, validationStore, "validation.unwaive", validation, Object{"reason": "Withdrawn"}, map[string]string{"context": token})

	checkpointBefore, _ := validationStore.Read()
	call(t, validationStore, "run.checkpointed", runID(claim), Object{"summary": "append-only"}, "context", token)
	call(t, validationStore, "run.checkpointed", runID(claim), Object{"summary": "append-only"}, "context", token)
	checkpointAfter, _ := validationStore.Read()
	if checkpointAfter.Revision != checkpointBefore.Revision+2 {
		t.Fatal("checkpoint stopped behaving as append-only audit history")
	}

	recordPass(t, validationStore, validation, token)
	call(t, validationStore, "task.completed", task, Object{"summary": "Done"}, "context", token)
	call(t, validationStore, "workstream.close", validationWorkstream, Object{})
	assertNoChange(t, validationStore, "workstream.close", validationWorkstream, Object{}, nil)
}

func TestValidationContextReasonsAndConditionalUse(t *testing.T) {
	for _, action := range []string{"validation.basis", "validation.record", "validation.accept", "validation.waive", "validation.unwaive"} {
		if def := Find(action); def == nil || !def.ContextOwner {
			t.Fatalf("%s is not marked as owner-contextual", action)
		}
	}

	s, workstream, task, validation := currentFixture(t)
	claim := call(t, s, "run.claimed", task, Object{})
	other := itemID(call(t, s, "task.add", "", Object{"title": "Other"}))
	otherClaim := call(t, s, "run.claimed", other, Object{})
	request := func(token string) *protocol.Error {
		_, e := s.Execute(context.Background(), Request{Action: "validation.basis", Target: validation, Body: Object{"code": []any{}}, Options: map[string]string{"request-id": ID(), "context": token}})
		return e
	}
	for token, reason := range map[string]string{"": "missing", "unknown": "unknown", str(otherClaim, "context"): "target_mismatch"} {
		if e := request(token); e == nil || e.Code != "context_invalid" || e.Details["context_reason"] != reason {
			t.Fatalf("validation context reason %s: %+v", reason, e)
		}
	}
	basisRequest := Request{Action: "validation.basis", Target: validation, Body: Object{"code": []any{}}, Options: map[string]string{"request-id": ID(), "context": str(claim, "context")}}
	basis, basisError := s.Execute(context.Background(), basisRequest)
	if basisError != nil {
		t.Fatal(basisError)
	}
	if basis["context_valid"] != true {
		t.Fatal("successful task validation did not confirm its context", basis)
	}
	call(t, s, "run.released", runID(claim), Object{}, "context", str(claim, "context"))
	replayedBasis, replayError := s.Execute(context.Background(), basisRequest)
	if replayError != nil || replayedBasis["replayed"] != true || replayedBasis["context_valid"] != false {
		t.Fatal("validation replay retained an inactive context", replayedBasis, replayError)
	}
	if e := request(str(claim, "context")); e == nil || e.Code != "context_invalid" || e.Details["context_reason"] != "inactive" {
		t.Fatal("inactive validation context was not classified", e)
	}

	integration := itemID(call(t, s, "validation.add", "", Object{"title": "Integration", "method": "test", "workstream_id": workstream}))
	state, _ := s.Read()
	result, e := s.Execute(context.Background(), Request{Action: "validation.basis", Target: integration, Body: Object{"code": []any{}}, Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision), "context": "irrelevant"}})
	if e != nil || result["context_valid"] != nil {
		t.Fatal("workstream validation consumed irrelevant context", result, e)
	}
}

func TestLegacyReceiptNormalizationAndIrrelevantContext(t *testing.T) {
	s := fixture(t)
	requestID := ID()
	request := Request{Action: "task.add", Body: Object{"title": "Legacy"}, Options: map[string]string{"request-id": requestID, "context": "ambient-old"}}
	created, e := s.Execute(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	createdID := itemID(created)

	var journal Journal
	if e := ReadPrivate(s.path(), &journal); e != nil {
		t.Fatal(e)
	}
	receipt := journal.Receipts[requestID]
	delete(receipt.Result, "previous_revision")
	delete(receipt.Result, "affected_ids")
	delete(receipt.Result, "affected_count")
	receipt.ContextHash = hash("ambient-old")
	journal.Receipts[requestID] = receipt
	if e := WritePrivate(s.path(), journal); e != nil {
		t.Fatal(e)
	}
	call(t, s, "task.add", "", Object{"title": "Advance"})

	request.Options["context"] = "ambient-new"
	replayed, e := s.Execute(context.Background(), request)
	if e != nil || replayed["replayed"] != true || num(replayed, "previous_revision") != 0 || num(replayed, "revision") != num(created, "revision") || num(replayed, "current_revision") <= num(replayed, "revision") || num(replayed, "affected_count") != 1 || !contains(arr(replayed, "affected_ids"), createdID) {
		t.Fatal("legacy context-free receipt was not normalized", replayed, e)
	}
}
