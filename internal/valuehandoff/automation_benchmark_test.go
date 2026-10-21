//go:build automationbench

package valuehandoff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

// Background cancel no-ops are real recipe-bound terminal receipts. They do
// not require thousands of live grants or relax any snapshot/session deadline.
func automationHandoffPopulation(b *testing.B, s Store, count int) {
	b.Helper()
	ctx := context.Background()
	r, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "population-job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(1000000))
	if e != nil {
		b.Fatal(e)
	}
	r, e = s.Cancel(ctx, r.Item.ID, requestID(1000001), RevisionInput{1})
	if e != nil {
		b.Fatal(e)
	}
	a, err := s.access(ctx, false)
	if err != nil {
		b.Fatal(err)
	}
	defer a.master.Destroy()
	gate, err := s.lockCommit(ctx, a)
	if err != nil {
		b.Fatal(err)
	}
	input := RevisionInput{r.Item.Revision}
	fp, err := fingerprint(a, "cancel", r.Item.ID, input)
	if err != nil {
		b.Fatal(err)
	}
	r.Changed = false
	guard, err := platformfs.OpenRoot(s.Root, false)
	if err != nil {
		b.Fatal(err)
	}
	defer guard.Close()
	if err := guard.MkdirAll(filepath.Dir(filepath.FromSlash(s.path("value-execution-requests", "")))); err != nil {
		b.Fatal(err)
	}
	for n := 1; n <= count-2; n++ {
		id := requestID(n)
		body, err := s.requestFile(ctx, a, id, fp, "cancel", r, false)
		if err != nil {
			b.Fatal(err)
		}
		// Setup only: each terminal no-op is independently authenticated and
		// atomically fsynced; no live profile/value update is being simulated.
		if err := guard.WriteFile(ctx, filepath.FromSlash(s.path("value-execution-requests", id)), body, false); err != nil {
			b.Fatal(err)
		}
		clear(body)
	}
	gate()
	for _, n := range []int{1, count - 2} {
		got, e := s.Cancel(ctx, r.Item.ID, requestID(n), input)
		if e != nil || !got.Replayed || got.Changed {
			b.Fatal("invalid seeded no-op receipt", e)
		}
	}
}

func benchmarkAutomationHandoff(b *testing.B, deliver bool) {
	for _, size := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("receipts_%d", size), func(b *testing.B) {
			s, _ := fixture(b)
			population := size
			if deliver {
				population -= 2
			}
			automationHandoffPopulation(b, s, population)
			ctx := context.Background()
			var accepted atomic.Int32
			var invalid atomic.Bool
			runner, instance := requestID(9000000), requestID(9000001)
			if deliver {
				receiverConfigFixture(b, s, Receiver{Root: s.Root, RunnerID: runner, InstanceID: instance,
					Accept: func(_ context.Context, p *ReceivedInput) error {
						env := p.Environment()
						if len(env) != 1 || env[0] != "TOKEN=synthetic-handoff-canary-TOKEN" {
							invalid.Store(true)
						}
						accepted.Add(1)
						return nil
					}, Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { return nil }})
				if _, e := s.Register(ctx, RunnerInput{runner, instance, 1, 1}, requestID(9000002)); e != nil {
					b.Fatal(e)
				}
			}
			starts := map[string]time.Time{}
			var held time.Duration
			s.deliveryHook = func(phase string) {
				switch phase {
				case "bench-gate-acquired", "bench-intent-gate-acquired":
					starts[phase] = time.Now()
				case "bench-gate-released":
					held += time.Since(starts["bench-gate-acquired"])
				case "bench-intent-gate-released":
					held += time.Since(starts["bench-intent-gate-acquired"])
				}
			}
			b.ReportAllocs()
			n := 0
			var totalHeld time.Duration
			for b.Loop() {
				b.StopTimer()
				ids := []string{requestID(2000000 + n*3), requestID(2000001 + n*3), requestID(2000002 + n*3)}
				var target Result
				if deliver {
					var pe *protocol.Error
					target, pe = s.Prepare(ctx, PrepareInput{ExternalExecutionID: fmt.Sprintf("measured-job-%d", n), Selection: Selection{Keys: []string{"TOKEN"}}}, ids[0])
					if pe != nil {
						b.Fatal(pe)
					}
					target, pe = s.Approve(ctx, target.Item.ID, ids[1], ApprovalInput{1, runner, instance, target.Item.ExternalExecutionID, target.Item.InputID})
					if pe != nil {
						b.Fatal(pe)
					}
				}
				entries, err := os.ReadDir(filepath.Dir(filepath.Join(s.Root, filepath.FromSlash(s.path("value-execution-requests", "")))))
				if err != nil || len(entries) != size {
					b.Fatal("receipt population mismatch", len(entries), err)
				}
				held = 0
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				b.StartTimer()
				start := time.Now()
				if deliver {
					r, e := s.Deliver(ctx, target.Item.ID, ids[2], RevisionInput{2})
					if e != nil || r.Item.DeliveryState != "delivered" || accepted.Load() != int32(n+1) || invalid.Load() {
						b.Fatal("measured delivery did not accept exactly the frozen input", e)
					}
				} else {
					r, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: fmt.Sprintf("measured-job-%d", n), Selection: Selection{Keys: []string{"TOKEN"}}}, ids[2])
					if e != nil || !r.Changed || r.Replayed || r.Item.DeliveryState != "prepared" {
						b.Fatal("measured preparation", e)
					}
					target = r
				}
				elapsed := time.Since(start)
				b.StopTimer()
				runtime.ReadMemStats(&after)
				if held <= 0 {
					b.Fatal("gate timing overlay was not installed")
				}
				totalHeld += held
				sample, _ := json.Marshal(map[string]any{"sample": n, "metrics": map[string]any{"ns/op": elapsed.Nanoseconds(), "B/op": after.TotalAlloc - before.TotalAlloc, "allocs/op": after.Mallocs - before.Mallocs, "maintenance-held-ns/op": held.Nanoseconds()}})
				b.Logf("AUTOMATION_SAMPLE %s", sample)
				files := map[string][]byte{s.path("value-executions", target.Item.ID): nil, s.path("value-execution-payloads", target.Item.ID): nil}
				for _, id := range ids {
					files[s.path("value-execution-requests", id)] = nil
				}
				if e := maintenance.Replace(s.Root, files); e != nil {
					b.Fatal(e)
				}
				n++
				b.StartTimer() // Raw operation samples exclude fixture restoration.
			}
			b.ReportMetric(float64(totalHeld.Nanoseconds())/float64(n), "maintenance-held-ns/op")
		})
	}
}
func BenchmarkAutomationPrepare(b *testing.B) { benchmarkAutomationHandoff(b, false) }
func BenchmarkAutomationDeliver(b *testing.B) { benchmarkAutomationHandoff(b, true) }
