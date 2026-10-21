package valuehandoff

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestReportResolvesLostACKWithoutRewritingCompletedUnknown(t *testing.T) {
	var effects atomic.Int32
	s, _, approved := deliveryFixture(t, Receiver{beforeACK: func(c vault.PeerConnection) { c.Close() }, Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { effects.Add(1); return nil }})
	ctx := context.Background()
	_, e := s.Deliver(ctx, approved.Item.ID, requestID(710), RevisionInput{2})
	if e == nil || e.Code != "delivery_outcome_unknown" {
		t.Fatal("lost ACK", e)
	}
	waitCount(t, &effects, 1)
	v, e := s.Status(ctx, approved.Item.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	input := ReportInput{Revision: v.Item.Revision, RunnerID: v.Item.RunnerID, ExternalExecutionID: v.Item.ExternalExecutionID, InputID: v.Item.InputID, State: "started", ExternalRef: "engine-recorded-job"}
	reported, e := s.Report(ctx, approved.Item.ID, requestID(711), input)
	if e != nil || reported.Item.DeliveryState != "delivered" || reported.Item.ExecutionState != "reported_started" {
		t.Fatal("reconcile", e)
	}
	if _, e = s.Deliver(ctx, approved.Item.ID, requestID(710), RevisionInput{2}); e == nil || e.Code != "delivery_outcome_unknown" {
		t.Fatal("completed unknown result rewritten", e)
	}
	if _, e = s.Deliver(ctx, approved.Item.ID, requestID(712), RevisionInput{reported.Item.Revision}); e == nil || e.Code != "delivery_already_started" {
		t.Fatal("reconciliation permitted another send", e)
	}
	if effects.Load() != 1 {
		t.Fatal("duplicate engine execution")
	}
}

func TestReportFinalizesAbandonedIntentAndKeepsOriginalRecipient(t *testing.T) {
	s, _, approved := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { return nil }})
	ctx := context.Background()
	// A real sender kill is covered by the D4 owned-process fixture. Here the
	// durable intent is reconstructed to isolate atomic report publication.
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	gate, err := s.lockCommit(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.readRecord(ctx, a, approved.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Item.DeliveryID = "0123456789abcdef0123456789abcdef"
	r.Item.DeliveryState, r.Item.ExecutionState = "delivering", "unknown"
	r.Item.Revision++
	r.DeliveryRequestID = requestID(720)
	fp, _ := fingerprint(a, "deliver", r.Item.ID, RevisionInput{2})
	files, err := s.recordFiles(ctx, a, r, r.DeliveryRequestID, fp, "deliver", Result{Profile: s.Profile, Item: r.Item, Changed: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = maintenance.Replace(s.Root, files); err != nil {
		t.Fatal(err)
	}
	gate()
	oldInstance := r.Item.RunnerInstanceID
	if _, e := s.Unregister(ctx, r.Item.RunnerID, oldInstance, requestID(721)); e != nil {
		t.Fatal(e)
	}
	next := requestID(722)
	receiverFixture(t, s, r.Item.RunnerID, next)
	if _, e := s.Register(ctx, RunnerInput{r.Item.RunnerID, next, 1, 1}, requestID(723)); e != nil {
		t.Fatal(e)
	}
	code := 0
	reported, e := s.Report(ctx, r.Item.ID, requestID(724), ReportInput{Revision: r.Item.Revision, RunnerID: r.Item.RunnerID, ExternalExecutionID: r.Item.ExternalExecutionID, InputID: r.Item.InputID, State: "finished", ExternalRef: "durable-engine-result", ExitCode: &code})
	if e != nil || reported.Item.ReporterInstanceID != next || reported.Item.RunnerInstanceID != oldInstance {
		t.Fatal("successor rebound recipient", e)
	}
	replay, e := s.Deliver(ctx, r.Item.ID, requestID(720), RevisionInput{2})
	if e != nil || !replay.Replayed || replay.Item.ExecutionState != "reported_finished" {
		t.Fatal("pending receipt was not finalized atomically", e)
	}
}
