package tasks

import (
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

func queryFingerprint(profile string, q Query) string {
	opts := Object{}
	for key, value := range q.Options {
		if key != "cursor" && key != "limit" {
			opts[key] = value
		}
	}
	return hash(Object{"command": q.Command, "target": q.Target, "options": opts, "profile": profile})
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
	var snapshot Snapshot
	if err := ReadPrivate(filepath.Join(s.cacheDirectory(), parts[0]+".json"), &snapshot); err != nil ||
		snapshot.Projection != ProjectionVersion ||
		snapshot.Fingerprint != queryFingerprint(s.Profile, q) ||
		time.Now().After(snapshot.Expires) {
		return nil, failure("cursor_invalid", "Start a new query.")
	}
	return pageSnapshotResult(s.Profile, snapshot, parts[0], offset, limit)
}

func replayGraphSnapshot(snapshot GraphSnapshot) (*State, *protocol.Error) {
	state := NewState()
	for _, event := range snapshot.Events {
		if event.Sequence != state.Revision+1 || safeApply(state, event) != nil {
			return nil, storageError()
		}
	}
	return state, nil
}

func (s Store) graphCursorFast(q Query, kind string, limit int) (Object, *protocol.Error) {
	token := q.Options["cursor"]
	if !validID(token) {
		return nil, failure("cursor_invalid", "Start a new graph query.")
	}
	var snapshot GraphSnapshot
	if err := ReadPrivate(filepath.Join(s.cacheDirectory(), token+".json"), &snapshot); err != nil ||
		snapshot.Projection != ProjectionVersion ||
		snapshot.Profile != s.Profile ||
		snapshot.Kind != kind ||
		time.Now().After(snapshot.Expires) {
		return nil, failure("cursor_invalid", "Start a new graph query.")
	}
	state, stateErr := replayGraphSnapshot(snapshot)
	if stateErr != nil {
		return nil, stateErr
	}
	if q.Target != "" {
		if _, err := state.Get(q.Target, kind); err != nil {
			return nil, err
		}
	}
	depth, err := positive(q.Options, "depth", 3, 20)
	if err != nil {
		return nil, err
	}
	direction := q.Options["direction"]
	if direction == "" {
		direction = "upstream"
	}
	if direction != "upstream" && direction != "downstream" {
		return nil, failure("invalid_argument", "Choose upstream or downstream.")
	}
	out := Object{"profile": s.Profile, "revision": state.Revision}
	for key, value := range state.Tree(kind, q.Target, q.Options["workstream"], direction, depth, limit) {
		out[key] = value
	}
	out["cursor"] = nil
	if out["truncated"] == true {
		out["cursor"] = token
	}
	return out, nil
}
