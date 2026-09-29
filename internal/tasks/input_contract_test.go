package tasks

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestNestedTextLimitsCountUnicodeCharacters(t *testing.T) {
	store := fixture(t)
	workstream := itemID(call(t, store, "workstream.create", "", Object{"title": "unicode"}))
	valid := strings.Repeat("가", 16384)
	call(t, store, "spec.set", workstream, Object{
		"body": "spec",
		"requirements": []any{
			Object{"key": "R", "text": valid},
		},
		"acceptance": []any{},
	})
	call(t, store, "task.add", "", Object{"title": "array", "acceptance": []any{valid}})

	state, readErr := store.Read(context.Background())
	if readErr != nil {
		t.Fatal(readErr)
	}
	_, err := store.Execute(context.Background(), Request{
		Action: "spec.set",
		Target: workstream,
		Body: Object{
			"body": "spec",
			"requirements": []any{
				Object{"key": "R", "text": valid + "가"},
			},
			"acceptance": []any{},
		},
		Options: map[string]string{"request-id": ID(), "if-revision": fmt.Sprint(state.Revision)},
	})
	if err == nil || err.Code != "invalid_argument" {
		t.Fatalf("oversized nested text accepted: %+v", err)
	}

	_, err = store.Execute(context.Background(), Request{
		Action:  "task.add",
		Body:    Object{"title": "too-long-array", "acceptance": []any{valid + "가"}},
		Options: map[string]string{"request-id": ID()},
	})
	if err == nil || err.Code != "invalid_argument" {
		t.Fatalf("oversized array text accepted: %+v", err)
	}
}

func TestCodeEvidenceStringUsesTextLengthLimit(t *testing.T) {
	valid := strings.Repeat("가", 16384)
	entry := func(evidence string) map[string]any {
		return map[string]any{"repository": "repo", "commit": nil, "dirty": false, "evidence": evidence}
	}
	if err := validateBody(Find("validation.basis"), Object{"code": []any{entry(valid)}}); err != nil {
		t.Fatalf("valid evidence rejected: %+v", err)
	}
	if err := validateBody(Find("validation.basis"), Object{"code": []any{entry(valid + "가")}}); err == nil || err.Code != "invalid_argument" {
		t.Fatalf("oversized evidence accepted: %+v", err)
	}
}
