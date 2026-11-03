package tasks

import (
	"github.com/jinyongp/devtools/internal/collectionquery"
	"github.com/jinyongp/devtools/internal/maintenance"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func queryKindCommand(command string) (string, string) {
	kind := "task"
	cmd := command
	if strings.HasPrefix(cmd, "workstream ") {
		kind = "workstream"
		cmd = strings.TrimPrefix(cmd, "workstream ")
	}
	if strings.HasPrefix(cmd, "validation ") {
		kind = "validation"
		cmd = strings.TrimPrefix(cmd, "validation ")
	}
	return kind, cmd
}

func pagedQueryCommand(cmd string) bool {
	switch cmd {
	case "list", "history", "current", "checkpoint list":
		return true
	default:
		return false
	}
}

func queryFingerprint(profile string, q Query, directories ...string) string {
	opts := Object{}
	definition := NavigationDefinition(q.Command)
	for key, value := range q.Options {
		if key != "profile" && key != "cursor" && key != "limit" && key != "query-cache-dir" && key != "columns" && key != "query" && key != "sort" && key != "order" && key != "since" && key != "until" && key != "time-field" {
			if _, filter := definition.Filters[key]; filter {
				continue
			}
			opts[key] = value
		}
	}
	query, _ := collectionquery.Parse(definition, q.Options, q.ListOptions)
	root := ""
	if len(directories) > 0 {
		root = filepath.Clean(maintenance.Root(directories[0]))
	}
	return hash(Object{"command": q.Command, "target": q.Target, "options": opts, "profile": profile, "query": query, "root": root})
}

func pageSnapshotResult(profile string, snapshot Snapshot, token string, offset, limit int) (Object, *protocol.Error) {
	if offset < 0 || offset > len(snapshot.Items) {
		return nil, failure("cursor_invalid", "Start a new query.")
	}
	end := offset + limit
	if end > len(snapshot.Items) {
		end = len(snapshot.Items)
	}
	out := Object{
		"profile":     profile,
		"revision":    snapshot.Revision,
		"items":       snapshot.Items[offset:end],
		"next_cursor": nil,
	}
	if end < len(snapshot.Items) {
		out["next_cursor"] = token + ":" + strconv.Itoa(end)
	}
	return out, nil
}

func (s Store) pageCursorFast(q Query, limit int) (Object, *protocol.Error) {
	parts := strings.Split(q.Options["cursor"], ":")
	if len(parts) != 2 || !validID(parts[0]) {
		return nil, failure("cursor_invalid", "Start a new query.")
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil || offset < 0 {
		return nil, failure("cursor_invalid", "Start a new query.")
	}
	f, err := s.openQueryCache(parts[0] + ".json")
	if err != nil {
		return nil, failure("cursor_invalid", "Start a new query.")
	}
	var magic [8]byte
	_, readErr := f.ReadAt(magic[:], 0)
	if readErr == nil && string(magic[:]) == queryCacheMagic {
		out, err := readQueryPage(f, s.Profile, queryFingerprint(s.Profile, q, s.Directory), parts[0], offset, limit)
		f.Close()
		if err != nil {
			return nil, failure("cursor_invalid", "Start a new query.")
		}
		return out, nil
	}
	f.Close()
	return nil, failure("cursor_invalid", "Start a new query; this snapshot format is no longer supported.")
}

func (s Store) graphCursorFast(q Query, kind string, limit int) (Object, *protocol.Error) {
	token := q.Options["cursor"]
	if !validID(token) {
		return nil, failure("cursor_invalid", "Start a new graph query.")
	}
	var snapshot GraphSnapshot
	if err := s.readQuerySnapshot(token+".json", &snapshot); err != nil ||
		snapshot.Version != graphSnapshotVersion ||
		snapshot.Nodes == nil || snapshot.Upstream == nil || snapshot.Downstream == nil ||
		snapshot.Projection != ProjectionVersion ||
		snapshot.Profile != s.Profile ||
		snapshot.Kind != kind ||
		time.Now().After(snapshot.Expires) {
		return nil, failure("cursor_invalid", "Start a new graph query.")
	}
	if q.Target != "" && snapshot.Nodes[q.Target] == nil {
		return nil, failure("not_found", "The requested item does not exist in this profile.")
	}
	direction, depth, err := graphOptions(q.Options)
	if err != nil {
		return nil, err
	}
	out := Object{"profile": s.Profile, "revision": snapshot.Revision}
	for key, value := range snapshot.tree(q.Target, q.Options["workstream"], direction, depth, limit) {
		out[key] = value
	}
	out["cursor"] = nil
	if out["truncated"] == true {
		out["cursor"] = token
	}
	return out, nil
}
