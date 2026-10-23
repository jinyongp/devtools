package values

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoveryUsesOriginalProfileAndRetryCondition(t *testing.T) {
	for _, tc := range []struct{ code, retry, command string }{{"revision_conflict", "after_refresh", "values revision"}, {"request_conflict", "inspect_first", "values request show"}} {
		e := protocol.NewError(tc.code, "State changed.", 3, nil)
		Recover(e, "/not-used", "actual", "local", "00000000-0000-4000-8000-000000000001", protocol.InvocationContext{IsolatedRoot: "/selected"})
		p := protocol.Public(e, "variable set")
		if p.Details["retry"] != tc.retry {
			t.Fatal(p)
		}
		r := p.Details["remedies"].([]protocol.Remedy)[0]
		argv := strings.Join(r.Argv, " ")
		if !strings.Contains(argv, tc.command) || !strings.Contains(argv, "--profile actual") || !strings.Contains(argv, "--isolated-root /selected") {
			t.Fatal(argv)
		}
	}
}

func TestSelectedSnapshotRetainsGlobalPrerequisiteMetadata(t *testing.T) {
	root, _, master, _ := vaultSnapshotFixture(t, 1)
	var calls atomic.Int32
	s, e := CaptureVault(context.Background(), root, "app", "local", true, time.Now(), snapshotMaster(master, &calls), "none")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Destroy()
	state := s.StateCopy()
	if len(state.Keys) != 0 {
		t.Fatal("excluded secret entered selected state")
	}
	p := state.Prerequisites()["SECRET_0000"]
	if p.Kind != Secret || p.Selected || !p.HasSlot {
		t.Fatal(p)
	}
}
