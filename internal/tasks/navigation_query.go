package tasks

import (
	"encoding/json"
	"strings"

	"github.com/jinyongp/devtools/internal/collectionquery"
	"github.com/jinyongp/devtools/internal/protocol"
)

// NavigationDefinition is shared with CLI metadata. The task projection owns
// its established ordering, scope and stale-completion rules before filtering.
func NavigationDefinition(command string) collectionquery.Definition {
	d := collectionquery.Definition{Search: []string{"title"}, Filters: map[string]string{}, Sorts: map[string]string{"name": "title", "created": "created_at", "updated": "updated_at"}, Times: map[string]string{"created": "created_at", "updated": "updated_at"}, Keys: []string{"id"}}
	switch command {
	case "list", "workstream list":
		d.Filters["state"] = "state"
	case "validation list":
		d.Filters["recorded-result"] = "recorded_result"
	case "current":
		d.Search = []string{"task_id"}
		d.Filters = map[string]string{"task": "task_id", "workstream": "workstream_id"}
		d.Sorts = map[string]string{"created": "started_at", "updated": "last_activity_at"}
		d.Times = map[string]string{"created": "started_at", "updated": "last_activity_at"}
	case "history", "workstream history", "checkpoint list":
		d.Search = []string{"action"}
		d.Filters = map[string]string{"action": "action"}
		d.Sorts = map[string]string{"created": "occurred_at"}
		d.Times = map[string]string{"created": "occurred_at"}
		d.DefaultSort, d.DefaultOrder = "created", "desc"
		if command == "checkpoint list" {
			d.Filters["kind"] = "action"
		}
	default:
		return collectionquery.Definition{}
	}
	return d
}

func queryStates(q Query) []string {
	if values := q.ListOptions["state"]; len(values) > 0 {
		return values
	}
	if value := q.Options["state"]; value != "" {
		return []string{value}
	}
	return nil
}

func filterNavigation(q Query, items []any) ([]any, *protocol.Error) {
	d := NavigationDefinition(q.Command)
	if len(d.Sorts) == 0 {
		return items, nil
	}
	parsed, err := collectionquery.Parse(d, q.Options, q.ListOptions)
	if err != nil {
		return nil, failure("invalid_argument", err.Error()+".")
	}
	// Run.MarshalJSON excludes execution-context hashes from public snapshots.
	b, err := json.Marshal(items)
	if err != nil {
		return nil, storageErrorAt("encode_query_rows", "task_query", err)
	}
	var rows []any
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.UseNumber()
	if err := decoder.Decode(&rows); err != nil {
		return nil, storageErrorAt("decode_query_rows", "task_query", err)
	}
	for i, row := range rows {
		if object, ok := row.(map[string]any); ok {
			object["_navigation_index"] = i
		} else {
			return nil, storageError()
		}
	}
	selected := collectionquery.Select(rows, d, parsed)
	result := make([]any, 0, len(selected))
	for _, row := range selected {
		result = append(result, items[row.(map[string]any)["_navigation_index"].(int)])
	}
	return result, nil
}
