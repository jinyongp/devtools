package valuehandoff

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
)

func TestReportsPreserveCompletedDeliveryAndTerminalResults(t *testing.T) {
	var effects atomic.Int32
	s, _, approved := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { effects.Add(1); return nil }})
	ctx := context.Background()
	input := ReportInput{Revision: 2, RunnerID: approved.Item.RunnerID, ExternalExecutionID: approved.Item.ExternalExecutionID, InputID: approved.Item.InputID, State: "started", ExternalRef: "engine-stable-1"}
	if _, e := s.Report(ctx, approved.Item.ID, requestID(700), input); e == nil || e.Code != "execution_state_conflict" {
		t.Fatal("report invented an attempt", e)
	}
	delivered, e := s.Deliver(ctx, approved.Item.ID, requestID(701), RevisionInput{2})
	if e != nil {
		t.Fatal(e)
	}
	input.Revision = delivered.Item.Revision
	started, e := s.Report(ctx, approved.Item.ID, requestID(700), input)
	if e != nil || started.Item.ExecutionState != "reported_started" || started.Item.FactSource != "runner_reported" {
		t.Fatal("start report", e)
	}
	replay, e := s.Report(ctx, approved.Item.ID, requestID(700), input)
	if e != nil || !replay.Replayed || replay.Item != started.Item {
		t.Fatal("report replay", e)
	}
	input.Revision = started.Item.Revision
	noop, e := s.Report(ctx, approved.Item.ID, requestID(702), input)
	if e != nil || noop.Changed {
		t.Fatal("identical report changed state", e)
	}
	input.State, input.Reason = "unknown", "runner_lost"
	if _, e := s.Report(ctx, approved.Item.ID, requestID(703), input); e == nil || e.Code != "execution_state_conflict" {
		t.Fatal("report regressed", e)
	}
	code := 0
	input.State, input.Reason, input.ExitCode = "finished", "", &code
	finished, e := s.Report(ctx, approved.Item.ID, requestID(704), input)
	if e != nil || finished.Item.ExecutionState != "reported_finished" {
		t.Fatal("finish report", e)
	}
	input.Revision = finished.Item.Revision
	code = 7
	if _, e := s.Report(ctx, approved.Item.ID, requestID(705), input); e == nil || e.Code != "execution_state_conflict" {
		t.Fatal("terminal conflict accepted", e)
	}
	replay, e = s.Deliver(ctx, approved.Item.ID, requestID(701), RevisionInput{2})
	if e != nil || !replay.Replayed || replay.Item != delivered.Item {
		t.Fatal("report rewrote completed delivery", e)
	}
	waitCount(t, &effects, 1)
}

func TestReportRejectsInvalidFactsBeforeMutation(t *testing.T) {
	s, _, r := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { return nil }})
	ctx := context.Background()
	delivered, e := s.Deliver(ctx, r.Item.ID, requestID(730), RevisionInput{2})
	if e != nil {
		t.Fatal(e)
	}
	base := ReportInput{Revision: delivered.Item.Revision, RunnerID: r.Item.RunnerID, ExternalExecutionID: r.Item.ExternalExecutionID, InputID: r.Item.InputID, State: "started", ExternalRef: "engine-ref"}
	for i, change := range []func(*ReportInput){
		func(p *ReportInput) { p.ExternalRef = "" },
		func(p *ReportInput) { p.State = "finished" },
		func(p *ReportInput) { p.State = "unknown" },
		func(p *ReportInput) { p.Reason = "raw engine exception" },
		func(p *ReportInput) { p.ExternalRef = "bad\nref" },
	} {
		p := base
		change(&p)
		if _, e := s.Report(ctx, r.Item.ID, requestID(731+i), p); e == nil || e.Code != "invalid_argument" {
			t.Fatal("invalid report accepted", i, e)
		}
	}
	wrong := base
	wrong.InputID = "00000000000000000000000000000000"
	if _, e := s.Report(ctx, r.Item.ID, requestID(740), wrong); e == nil || e.Code != "execution_state_conflict" {
		t.Fatal("wrong input report", e)
	}
	failed := base
	failed.State, failed.Reason, failed.ExternalRef = "failed", "runner_start_failed", ""
	result, e := s.Report(ctx, r.Item.ID, requestID(741), failed)
	if e != nil || result.Item.DeliveryState != "delivered" || result.Item.ExecutionState != "reported_failed" {
		t.Fatal("execution failure erased delivery fact", e)
	}
	base.Revision = result.Item.Revision
	if _, e := s.Report(ctx, r.Item.ID, requestID(742), base); e == nil || e.Code != "execution_state_conflict" {
		t.Fatal("terminal failure restarted", e)
	}
}
