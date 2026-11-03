package collectionquery

import (
	"reflect"
	"testing"
)

func TestSelectionFiltersBeforeStableSortAndUsesHalfOpenRecordedTime(t *testing.T) {
	d := Definition{Search: []string{"command"}, Filters: map[string]string{"state": "state", "env": "env"}, Sorts: map[string]string{"created": "created_at"}, Times: map[string]string{"created": "created_at", "ended": "ended_at"}, Keys: []string{"id"}, DefaultSort: "created", DefaultOrder: "desc"}
	rows := []any{
		map[string]any{"id": "c", "command": "Web API", "env": "local", "state": "running", "created_at": "2026-01-01T02:00:00Z", "ended_at": nil},
		map[string]any{"id": "b", "command": "Web API", "env": "local", "state": "failed", "created_at": "2026-01-01T01:00:00Z", "ended_at": "2026-01-01T01:00:00Z"},
		map[string]any{"id": "a", "command": "Web API", "env": "local", "state": "failed", "created_at": "2026-01-01T01:00:00Z", "ended_at": "2026-01-01T01:00:00Z"},
		map[string]any{"id": "d", "command": "worker", "env": "local", "state": "failed", "created_at": "2026-01-01T01:00:00Z"},
		map[string]any{"id": "e", "command": "Web API", "env": "other", "state": "running", "created_at": "2026-01-01T01:00:00Z"},
	}
	q, err := Parse(d, map[string]string{"query": "  WEB api ", "env": "local", "since": "2026-01-01T01:00:00Z", "until": "2026-01-01T02:00:00Z"}, map[string][]string{"state": {"running", "failed"}})
	if err != nil {
		t.Fatal(err)
	}
	selected := Select(rows, d, q)
	if len(selected) != 2 || String(selected[0], "id") != "a" || String(selected[1], "id") != "b" {
		t.Fatal(selected)
	}
	q.TimeField, q.Until = "ended", nil
	if got := Select(rows, d, q); !reflect.DeepEqual(got, selected) {
		t.Fatal("missing ended time must not be inferred", got)
	}
	if String(rows[0], "id") != "c" {
		t.Fatal("selection mutated source order")
	}
}

func TestQueryBoundsAndNoPrivateFieldSearch(t *testing.T) {
	d := Definition{Search: []string{"name"}, Sorts: map[string]string{"name": "name"}, DefaultSort: "name"}
	for _, options := range []map[string]string{{"limit": "0"}, {"limit": "201"}, {"order": "random"}, {"sort": "secret"}, {"since": "2026-01-01T00:00:00"}, {"time-field": "ended"}} {
		if _, e := Parse(d, options, nil); e == nil {
			t.Fatal(options)
		}
	}
	q, e := Parse(d, map[string]string{"query": "PRIVATE-CANARY"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if got := Select([]any{map[string]any{"name": "public", "value": "PRIVATE-CANARY", "directory": "PRIVATE-CANARY", "logs": "PRIVATE-CANARY"}}, d, q); len(got) != 0 {
		t.Fatal(got)
	}
}
