package valuehandoff

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *safeBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func deliveryFixture(t *testing.T, config Receiver) (Store, *vault.SessionClient, Result) {
	t.Helper()
	s, c := fixture(t)
	ctx := context.Background()
	runner, instance := requestID(600), requestID(601)
	config.Root = s.Root
	config.RunnerID = runner
	config.InstanceID = instance
	receiverConfigFixture(t, s, config)
	if _, e := s.Register(ctx, RunnerInput{runner, instance, 1, 1}, requestID(602)); e != nil {
		t.Fatal(e)
	}
	prepared, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "delivery-job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(603))
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Approve(ctx, prepared.Item.ID, requestID(604), ApprovalInput{1, runner, instance, prepared.Item.ExternalExecutionID, prepared.Item.InputID})
	if e != nil {
		t.Fatal(e)
	}
	return s, c, r
}
func waitCount(t *testing.T, count *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for count.Load() != want {
		if time.Now().After(deadline) {
			t.Fatal("receiver did not observe expected acceptance count")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestPrivateDeliveryOnceWithProtectedFirstOutput(t *testing.T) {
	var count atomic.Int32
	var out, diagnostic safeBuffer
	s, _, approved := deliveryFixture(t, Receiver{Stdout: &out, Stderr: &diagnostic, Handle: func(ctx context.Context, input *ReceivedInput, out, diagnostic io.Writer) error {
		env := input.Environment()
		if len(env) != 1 || env[0] != "TOKEN=synthetic-handoff-canary-TOKEN" {
			t.Error("receiver got different frozen bytes")
		}
		if _, err := json.Marshal(input); err == nil {
			t.Error("received input allowed JSON serialization")
		}
		if fmt.Sprintf("%+v", input) != "[REDACTED]" {
			t.Error("received input allowed fmt exposure")
		}
		io.WriteString(out, "first:synthetic-handoff-canary-TOKEN\n")
		io.WriteString(diagnostic, "synthetic-handoff-canary-TOKEN\n")
		count.Add(1)
		return nil
	}})
	ctx := context.Background()
	gateFree := false
	s.deliveryHook = func(phase string) {
		if phase == "intent" {
			check, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			release, err := maintenance.AcquireExclusive(check, s.Root)
			if err != nil {
				t.Error("runner I/O retained maintenance gate", err)
				return
			}
			gateFree = true
			release()
		}
	}
	r, e := s.Deliver(ctx, approved.Item.ID, requestID(610), RevisionInput{2})
	if e != nil || r.Item.DeliveryState != "delivered" || r.Item.Revision != 4 {
		t.Fatal("delivery failed", e)
	}
	waitCount(t, &count, 1)
	if !gateFree {
		t.Fatal("intent checkpoint not reached")
	}
	replay, e := s.Deliver(ctx, approved.Item.ID, requestID(610), RevisionInput{2})
	if e != nil || !replay.Replayed || replay.Item != r.Item {
		t.Fatal("delivery retry changed saved outcome", e)
	}
	if _, e = s.Deliver(ctx, approved.Item.ID, requestID(611), RevisionInput{4}); e == nil || e.Code != "delivery_already_started" {
		t.Fatal("new UUID resent accepted input", e)
	}
	if _, e = s.Cancel(ctx, approved.Item.ID, requestID(612), RevisionInput{4}); e == nil || e.Code != "delivery_already_started" {
		t.Fatal("delivery described as revoked", e)
	}
	if count.Load() != 1 {
		t.Fatal("duplicate receiver execution")
	}
	if _, err := os.Stat(filepath.Join(s.Root, s.path("value-execution-payloads", approved.Item.ID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("delivered snapshot ciphertext survived", err)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(out.String(), "[REDACTED]") {
		if time.Now().After(deadline) {
			t.Fatal("protected output missing")
		}
		time.Sleep(time.Millisecond)
	}
	if strings.Contains(out.String(), "canary") || strings.Contains(diagnostic.String(), "canary") {
		t.Fatal("first output exposed secret")
	}
	b, _ := json.Marshal(r)
	for _, private := range []string{"canary", "patterns", "ticket", "endpoint"} {
		if bytes.Contains(b, []byte(private)) {
			t.Fatal("public delivery response exposed private input")
		}
	}
}
func TestLostACKIsUnknownAndNeverResends(t *testing.T) {
	var count atomic.Int32
	s, c, approved := deliveryFixture(t, Receiver{beforeACK: func(c vault.PeerConnection) { c.Close() }, Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { count.Add(1); return nil }})
	ctx := context.Background()
	if _, e := s.Deliver(ctx, approved.Item.ID, requestID(620), RevisionInput{2}); e == nil || e.Code != "delivery_outcome_unknown" {
		t.Fatal("missing ACK was not unknown", e)
	}
	waitCount(t, &count, 1)
	for _, id := range []string{requestID(620), requestID(621)} {
		if _, e := s.Deliver(ctx, approved.Item.ID, id, RevisionInput{2}); e == nil || e.Code != "delivery_outcome_unknown" {
			t.Fatal("unknown input retried as new work", e)
		}
	}
	if count.Load() != 1 {
		t.Fatal("retry increased receiver calls")
	}
	view, e := s.Status(ctx, approved.Item.ID, "")
	if e != nil || view.Item.DeliveryState != "unknown" || view.Item.ExecutionState != "unknown" {
		t.Fatal("lost ACK state wrong", e)
	}
	entries, err := os.ReadDir(filepath.Join(s.Root, "vault", "launches"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("external unknown created own-process launch journal")
	}
	lock, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := c.Lock(lock); err != nil {
		t.Fatal("external unknown blocked lock", err)
	}
}
func TestLockAndUnregisterBeforeAdmissionSendNoValues(t *testing.T) {
	for _, action := range []string{"lock", "unregister"} {
		t.Run(action, func(t *testing.T) {
			var count atomic.Int32
			s, c, approved := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { count.Add(1); return nil }})
			ctx := context.Background()
			s.deliveryHook = func(phase string) {
				if phase != "ready" {
					return
				}
				if action == "lock" {
					if err := c.Lock(ctx); err != nil {
						t.Error(err)
					}
				} else {
					if _, e := s.Unregister(ctx, approved.Item.RunnerID, approved.Item.RunnerInstanceID, requestID(630)); e != nil {
						t.Error(e)
					}
				}
			}
			if _, e := s.Deliver(ctx, approved.Item.ID, requestID(631), RevisionInput{2}); e == nil {
				t.Fatal("revoked admission succeeded")
			}
			if count.Load() != 0 {
				t.Fatal("value-bearing input escaped before admission")
			}
			if _, err := os.Stat(filepath.Join(s.Root, s.path("value-execution-requests", requestID(631)))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("pre-intent rejection consumed request UUID", err)
			}
		})
	}
}

func TestSelectionAloneCannotDeliverAndLimitsBindNoIntent(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	r, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "no-approval", Selection: Selection{Keys: []string{}}}, requestID(640))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Deliver(ctx, r.Item.ID, requestID(641), RevisionInput{1}); e == nil || e.Code != "approval_required" {
		t.Fatal("selection granted delivery authority", e)
	}
	var count atomic.Int32
	s, _, approved := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { count.Add(1); return nil }})
	// Corrupt the prepared ciphertext: it is rejected before durable intent.
	path := s.path("value-execution-payloads", approved.Item.ID)
	gate, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	err = maintenance.Replace(s.Root, map[string][]byte{path: []byte(`{}`)})
	gate()
	if err != nil {
		t.Fatal(err)
	}
	if _, e = s.Deliver(ctx, approved.Item.ID, requestID(642), RevisionInput{2}); e == nil {
		t.Fatal("corrupt snapshot delivered")
	}
	if count.Load() != 0 {
		t.Fatal("corrupt input reached receiver")
	}
	if _, err = os.Stat(filepath.Join(s.Root, s.path("value-execution-requests", requestID(642)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed preparation reserved delivery UUID", err)
	}
}

func TestAdmittedSendDoesNotIndefinitelyBlockLock(t *testing.T) {
	accepted, ack := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(ack) }) }
	defer unblock()
	var count atomic.Int32
	s, c, approved := deliveryFixture(t, Receiver{beforeACK: func(vault.PeerConnection) { close(accepted); <-ack }, Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { count.Add(1); return nil }})
	ctx := context.Background()
	finished := make(chan struct{})
	go func() { defer close(finished); s.Deliver(ctx, approved.Item.ID, requestID(650), RevisionInput{2}) }()
	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("receiver never accepted private frame")
	}
	lock, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	err := c.Lock(lock)
	cancel()
	if err == nil {
		unblock()
		t.Fatal("lock acknowledged before admitted send finished")
	}
	unblock()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("admitted send did not release physical leases")
	}
	waitCount(t, &count, 1)
	lock, cancel = context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = c.Lock(lock); err != nil {
		t.Fatal("completed external send blocked durable lock", err)
	}
}

func TestLockWinsFinalizationAndSavedIntentStillPreventsResend(t *testing.T) {
	var count atomic.Int32
	s, c, approved := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { count.Add(1); return nil }})
	ctx := context.Background()
	s.deliveryHook = func(phase string) {
		if phase == "finalize" {
			if err := c.Lock(ctx); err != nil {
				t.Error(err)
			}
		}
	}
	if _, e := s.Deliver(ctx, approved.Item.ID, requestID(660), RevisionInput{2}); e == nil || e.Code != "delivery_outcome_unknown" {
		t.Fatal("finalization failure was treated as known", e)
	}
	waitCount(t, &count, 1)
	pass := vault.NewMaterial([]byte("synthetic-handoff-passphrase"))
	defer pass.Destroy()
	if err := c.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	view, e := s.Status(ctx, approved.Item.ID, "")
	if e != nil || view.Item.DeliveryState != "unknown" {
		t.Fatal("failed finalization lost durable intent", e)
	}
	s.deliveryHook = nil
	for _, request := range []string{requestID(660), requestID(661)} {
		if _, e = s.Deliver(ctx, approved.Item.ID, request, RevisionInput{2}); e == nil || e.Code != "delivery_outcome_unknown" {
			t.Fatal("relock enabled another send", e)
		}
	}
	if count.Load() != 1 {
		t.Fatal("finalization retry increased acceptances")
	}
}

func TestCanceledBlockedACKReleasesLeasesAndDoesNotResend(t *testing.T) {
	accepted, ack := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(ack) }) }
	defer unblock()
	var count atomic.Int32
	s, c, approved := deliveryFixture(t, Receiver{beforeACK: func(vault.PeerConnection) { close(accepted); <-ack }, Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { count.Add(1); return nil }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan string, 1)
	go func() {
		_, e := s.Deliver(ctx, approved.Item.ID, requestID(670), RevisionInput{2})
		if e == nil {
			finished <- ""
		} else {
			finished <- e.Code
		}
	}()
	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("receiver never accepted payload")
	}
	cancel()
	select {
	case code := <-finished:
		if code != "delivery_outcome_unknown" {
			t.Fatal("canceled sensitive send was retried or inferred", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled sender retained physical leases")
	}
	lock, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := c.Lock(lock); err != nil {
		t.Fatal("blocked ACK left admission leases", err)
	}
	unblock()
	waitCount(t, &count, 1)
}
