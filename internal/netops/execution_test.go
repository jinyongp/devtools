package netops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestExecutionRecoveryPreservesFingerprintAndCaller(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	caller := t.TempDir()
	_, e := s.ChooseExecution(context.Background(), id, "input", network.PrepareAuto, func() (ExecutionPlan, *protocol.Error) { return ExecutionPlan{}, nil }, caller)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(s.Data, "network-operations", id+".json")
	before, _ := os.ReadFile(path)
	r, found, e := s.ReadExecution(id)
	if e != nil || !found || r.Caller != caller || r.Validate("input", network.PrepareAuto, caller) != nil {
		t.Fatal(r, found, e)
	}
	if r.Validate("different", network.PrepareAuto, caller) == nil || r.Validate("input", network.PrepareOff, caller) == nil || r.Validate("input", network.PrepareAuto, t.TempDir()) == nil {
		t.Fatal("recovery bypassed input identity")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("read changed journal")
	}
	encoded, _ := json.Marshal(executionInput{Action: "execution-request-v1", Mode: network.PrepareAuto, Input: "input", Caller: caller})
	if string(encoded) != `{"Action":"execution-request-v1","Mode":"auto","Input":"input"}` {
		t.Fatal("legacy fingerprint projection changed", string(encoded))
	}
}

func TestExecutionIntentConcurrentReplayAndChangedMode(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	var calls atomic.Int32
	choose := func() ([]string, *protocol.Error) { calls.Add(1); return []string{"web"}, nil }
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := s.PlanExecution(context.Background(), id, "input", network.PrepareAuto, choose)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- p.NetworkID
		}()
	}
	wg.Wait()
	close(ids)
	previous := ""
	for id := range ids {
		if previous != "" && previous != id {
			t.Fatal("changed child identity")
		}
		previous = id
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	if _, e := s.PlanExecution(context.Background(), id, "input", network.PrepareOff, choose); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
}

func TestFailedSelectionRetainsNewIntentAndAllowsSameRequestRetry(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	if exists, intent, e := s.ExecutionKind(id); e != nil || exists || intent {
		t.Fatal(exists, intent, e)
	}
	_, e := s.PlanExecution(context.Background(), id, "input", network.PrepareAuto, func() ([]string, *protocol.Error) { return nil, Failure("permission_required", "not ready") })
	if e == nil {
		t.Fatal("expected failure")
	}
	if exists, intent, e := s.ExecutionKind(id); e != nil || !exists || !intent {
		t.Fatal(exists, intent, e)
	}
	p, e := s.PlanExecution(context.Background(), id, "input", network.PrepareAuto, func() ([]string, *protocol.Error) { return []string{"web"}, nil })
	if e != nil || len(p.Commands) != 1 {
		t.Fatal(p, e)
	}
	legacy := tasks.ID()
	if _, e := s.Apply(context.Background(), legacy, "legacy", nil, func(*Journal) (any, *protocol.Error) { return map[string]any{}, nil }); e != nil {
		t.Fatal(e)
	}
	if exists, intent, e := s.ExecutionKind(legacy); e != nil || !exists || intent {
		t.Fatal(exists, intent, e)
	}
}
