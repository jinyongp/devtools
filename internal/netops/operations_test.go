package netops

import (
	"context"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestReplayConflictAndIncompletePhaseResume(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	calls := 0
	fn := func(j *Journal) (any, *protocol.Error) {
		calls++
		var phase string
		present, e := j.Get("phase", &phase)
		if e != nil {
			return nil, e
		}
		if !present {
			if e := j.Set("phase", "prepared"); e != nil {
				return nil, e
			}
			return nil, Failure("interrupted", "retry")
		}
		if phase != "prepared" {
			t.Fatal(phase)
		}
		return map[string]any{"changed": true}, nil
	}
	input := map[string]string{"action": "test"}
	if _, e := s.Apply(context.Background(), id, input, []string{"b", "a"}, fn); e == nil {
		t.Fatal("first interruption hidden")
	}
	if _, e := s.Apply(context.Background(), id, input, []string{"a", "b"}, fn); e != nil {
		t.Fatal(e)
	}
	r, e := s.Apply(context.Background(), id, input, nil, fn)
	if e != nil || r.(map[string]any)["replayed"] != true || calls != 2 {
		t.Fatal(r, e, calls)
	}
	if _, e := s.Apply(context.Background(), id, map[string]string{"action": "other"}, nil, fn); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
}
