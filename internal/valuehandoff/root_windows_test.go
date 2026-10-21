package valuehandoff

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWindowsCaseAliasedReceiverRootUsesCanonicalEndpoint(t *testing.T) {
	s, _ := fixture(t)
	alias := strings.ToUpper(s.Root)
	canonical, err := CanonicalRoot(alias)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != s.Root {
		t.Fatal("same physical root produced another canonical identity")
	}
	runner, instance := requestID(800), requestID(801)
	var calls atomic.Int32
	receiverConfigFixture(t, s, Receiver{Root: alias, RunnerID: runner, InstanceID: instance, Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { calls.Add(1); return nil }})
	ctx := context.Background()
	if _, e := s.Register(ctx, RunnerInput{runner, instance, 1, 1}, requestID(802)); e != nil {
		t.Fatal(e)
	}
	prepared, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "case-root-job", Selection: Selection{Keys: []string{}}}, requestID(803))
	if e != nil {
		t.Fatal(e)
	}
	approved, e := s.Approve(ctx, prepared.Item.ID, requestID(804), ApprovalInput{1, runner, instance, prepared.Item.ExternalExecutionID, prepared.Item.InputID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Deliver(ctx, prepared.Item.ID, requestID(805), RevisionInput{approved.Item.Revision}); e != nil {
		t.Fatal("case alias split the private channel", e)
	}
	waitCount(t, &calls, 1)
}
