package tasks

import (
	"errors"
	"sort"
)

// Only the disposable snapshot uses this format. The WAL, history responses,
// and definition signatures continue to contain the original document bodies.
const compactSnapshotVersion = 4

func snapshotBodyObjects(m *materializedState, visit func(Object) error) error {
	ids := make([]string, 0, len(m.Items))
	for id := range m.Items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := m.Items[id]
		if item == nil || item.Kind != "workstream" {
			continue
		}
		for _, key := range []string{"spec", "plan"} {
			if doc := snapshotObject(item.Props[key]); doc != nil {
				if err := visit(doc); err != nil {
					return err
				}
			}
		}
	}
	sequences := make([]int, 0, len(m.HistoryEvents))
	for sequence := range m.HistoryEvents {
		sequences = append(sequences, sequence)
	}
	sort.Ints(sequences)
	for _, sequence := range sequences {
		event := m.HistoryEvents[sequence]
		if event.Action == "spec.set" || event.Action == "plan.set" {
			if err := visit(event.Data); err != nil {
				return err
			}
		}
		if event.Action == "workstream.edited" {
			for _, patch := range objects(event.Data, "patches") {
				if str(patch, "kind") != "workstream" {
					continue
				}
				props := snapshotObject(patch["props"])
				for _, key := range []string{"spec", "plan"} {
					if doc := snapshotObject(props[key]); doc != nil {
						if err := visit(doc); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func snapshotObject(value any) Object {
	switch v := value.(type) {
	case Object:
		return v
	case map[string]any:
		return Object(v)
	}
	return nil
}

func compactSnapshotBodies(m *materializedState) {
	indices := map[string]int{}
	_ = snapshotBodyObjects(m, func(doc Object) error {
		body, ok := doc["body"].(string)
		if !ok || len(body) < 1024 {
			return nil
		}
		index, exists := indices[body]
		if !exists {
			index = len(m.DocumentBodies)
			indices[body] = index
			m.DocumentBodies = append(m.DocumentBodies, body)
		}
		doc["body"] = Object{"snapshot_body": index}
		return nil
	})
	if m.DocumentBodies != nil {
		m.FormatVersion = compactSnapshotVersion
	}
}

func expandSnapshotBodies(m *materializedState) error {
	if m.FormatVersion == taskStorageVersion {
		if m.DocumentBodies != nil {
			return errors.New("unexpected snapshot document table")
		}
		return nil
	}
	if len(m.DocumentBodies) == 0 {
		return errors.New("missing snapshot document table")
	}
	// Leave the encoded snapshot intact for callers checking/repairing its
	// checksum; restore documents only in the request's mutable projection.
	items := make(map[string]*Item, len(m.Items))
	for id, item := range m.Items {
		items[id] = copyItem(item)
	}
	m.Items = items
	events := make(map[int]Event, len(m.HistoryEvents))
	for sequence, event := range m.HistoryEvents {
		events[sequence] = copyEvent(event)
	}
	if m.HistoryEvents != nil {
		m.HistoryEvents = events
	}
	return snapshotBodyObjects(m, func(doc Object) error {
		ref := snapshotObject(doc["body"])
		if ref == nil {
			return nil
		}
		value, ok := ref["snapshot_body"]
		if !ok || len(ref) != 1 {
			return errors.New("invalid snapshot document reference")
		}
		var index int
		switch v := value.(type) {
		case int:
			index = v
		case float64:
			if v < 0 || v >= float64(len(m.DocumentBodies)) || v != float64(int(v)) {
				return errors.New("invalid snapshot document index")
			}
			index = int(v)
		default:
			return errors.New("invalid snapshot document index")
		}
		if index < 0 || index >= len(m.DocumentBodies) {
			return errors.New("invalid snapshot document index")
		}
		doc["body"] = m.DocumentBodies[index]
		return nil
	})
}
