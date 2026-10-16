package tasks

import (
	"time"

	"github.com/jinyongp/devtools/internal/retention"
)

func (s *State) graphSnapshot(profile, kind string) GraphSnapshot {
	s.prepareReads()
	snapshot := GraphSnapshot{
		Version: graphSnapshotVersion, Projection: ProjectionVersion,
		Profile: profile, Kind: kind, Revision: s.Revision,
		Expires: time.Now().Add(retention.QuerySnapshotTTL),
		Nodes:   map[string]Object{}, OrderedIDs: []string{},
		Upstream: map[string][]string{}, Downstream: map[string][]string{},
	}
	// Preserve the entire kind, including removed nodes that may be referenced by
	// a continuation. Views are detached from State and never refreshed on replay.
	for _, item := range s.readIndex.ordered[kind] {
		snapshot.Nodes[item.ID] = copyObject(s.View(item))
		snapshot.Upstream[item.ID] = append([]string{}, item.Depends...)
		snapshot.Downstream[item.ID] = s.downstream(item.ID, kind)
		if s.Included(item) {
			snapshot.OrderedIDs = append(snapshot.OrderedIDs, item.ID)
		}
	}
	return snapshot
}

func (s GraphSnapshot) tree(id, workstream, direction string, depth, limit int) Object {
	graph := graphReader{
		has:  func(id string) bool { return s.Nodes[id] != nil },
		view: func(id string) Object { return s.Nodes[id] },
		ordered: func() []string {
			ids := []string{}
			for _, id := range s.OrderedIDs {
				if workstream == "" || str(s.Nodes[id], "workstream_id") == workstream {
					ids = append(ids, id)
				}
			}
			return ids
		},
		neighbors: func(id string) []string {
			if direction == "downstream" {
				return s.Downstream[id]
			}
			return s.Upstream[id]
		},
	}
	return graph.tree(id, direction, depth, limit)
}
