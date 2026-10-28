package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestUncertainChildRetryPreservesRequestIdentity(t *testing.T) {
	for _, action := range []string{ActionUp, ActionRestart} {
		for _, proxy := range []bool{false, true} {
			for _, outcome := range []string{"unknown", "interrupted", "uncertain_reason", "observed_interrupted"} {
				t.Run(fmt.Sprintf("%s/proxy=%t/%s", action, proxy, outcome), func(t *testing.T) {
					p := testProject(t, "web")
					old := runningRecord(p, "web", tasks.ID())
					old.ProxyMode = proxy
					f := &fakeProcesses{records: []services.Record{old}}
					f.apply = func(q services.Request, _ int) (services.Result, *protocol.Error) {
						r := runningRecord(p, "web", q.RequestID)
						r.InputSnapshotID = strings.Repeat("a", 32)
						r.State = outcome
						if outcome == "uncertain_reason" {
							r.State, r.Reason = "failed", "launch_outcome_unknown"
						}
						if outcome == "observed_interrupted" {
							r.State = "interrupted"
							return services.Result{Item: r}, nil
						}
						return services.Result{Item: r}, protocol.NewError("launch_outcome_unknown", "Inspect the original execution.", 3, nil)
					}
					m := Manager{Data: t.TempDir(), Processes: f, PrepareNetwork: func(context.Context, Request, []services.Record, string) (map[string]*network.ExecutionSnapshot, *protocol.Error) {
						return map[string]*network.ExecutionSnapshot{"web": {ID: tasks.ID()}}, nil
					}}
					q := Request{Action: action, Project: p, Commands: []string{"web"}, ProxyMode: proxy && action == ActionUp, Timeout: time.Second, RequestID: tasks.ID()}
					for range 2 {
						if _, e := m.Apply(context.Background(), q); e == nil {
							t.Fatal("unknown launch became successful")
						}
					}
					if len(f.calls) != 2 || f.calls[0].RequestID != f.calls[1].RequestID || f.calls[0].ID != f.calls[1].ID {
						t.Fatal("uncertain retry changed the original mutation identity", f.calls)
					}
					value, err := m.readReceipt(q.RequestID)
					if err != nil || value.complete() {
						t.Fatal("uncertain outcome became completed", err)
					}
				})
			}
		}
	}
}
