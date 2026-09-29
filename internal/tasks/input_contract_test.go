package tasks

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestWALJSONPreservesHTMLCharacters(t *testing.T) {
	body, err := marshalWALSubrecord(Object{"body": "<>&"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "\\u003c") || strings.Contains(string(body), "\\u003e") || strings.Contains(string(body), "\\u0026") {
		t.Fatalf("WAL JSON expanded HTML characters: %s", body)
	}
}

func TestValidHTMLHeavyDocumentPersists(t *testing.T) {
	store := fixture(t)
	workstream := itemID(call(t, store, "workstream.create", "", Object{"title": "large"}))
	body := strings.Repeat("<", 1<<20)
	result := call(t, store, "spec.set", workstream, Object{
		"body":         body,
		"requirements": []any{},
		"acceptance":   []any{},
	})
	if result["changed"] != true {
		t.Fatalf("large document did not persist: %#v", result)
	}
	state, err := store.Read(context.Background())
	if err != nil || str(objectValue(state.Items[workstream].Props["spec"]), "body") != body {
		t.Fatalf("large document changed after reload: %v", err)
	}
}

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

func TestValidEditWithLineSeparatorsFitsWAL(t *testing.T) {
	store := fixture(t)
	workstream := itemID(call(t, store, "workstream.create", "", Object{"title": "unicode wal"}))
	prefix := `{"reason":"r","operations":[{"op":"spec.update","value":{"body":"`
	middle := `"}},{"op":"plan.update","value":{"body":"`
	suffix := `"}}]}`
	separator := string(rune(0x2028))
	available := (2 << 20) - len(prefix) - len(middle) - len(suffix) - 1
	count := available / (2 * len(separator))
	document := strings.Repeat(separator, count)
	raw := prefix + document + middle + document + suffix
	if len(raw) > 2<<20 || len(document) > 1<<20 {
		t.Fatalf("invalid test boundary: raw=%d document=%d", len(raw), len(document))
	}
	body, decodeErr := Decode(raw)
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	state, readErr := store.Read(context.Background())
	if readErr != nil {
		t.Fatal(readErr)
	}
	_, execErr := store.Execute(context.Background(), Request{
		Action: "workstream.edited",
		Target: workstream,
		Body:   body,
		Options: map[string]string{
			"request-id":  ID(),
			"if-revision": fmt.Sprint(state.Revision),
		},
	})
	if execErr != nil {
		t.Fatalf("valid public edit did not persist: %+v", execErr)
	}
}
