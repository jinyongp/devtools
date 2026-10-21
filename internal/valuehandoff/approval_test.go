package valuehandoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

func approvedFixture(t *testing.T) (Store, *vault.SessionClient, Result, ApprovalInput) {
	t.Helper()
	s, c := fixture(t)
	ctx := context.Background()
	runner, instance := requestID(500), requestID(501)
	receiverFixture(t, s, runner, instance)
	if _, e := s.Register(ctx, RunnerInput{runner, instance, 1, 1}, requestID(502)); e != nil {
		t.Fatal(e)
	}
	r, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "approved-job", Selection: Selection{Keys: []string{"TOKEN"}}}, requestID(503))
	if e != nil {
		t.Fatal(e)
	}
	return s, c, r, ApprovalInput{r.Item.Revision, runner, instance, r.Item.ExternalExecutionID, r.Item.InputID}
}
func TestExplicitApprovalCancellationAndSavedResults(t *testing.T) {
	s, c, prepared, input := approvedFixture(t)
	ctx := context.Background()
	if prepared.Item.RunnerID != "" || prepared.Item.DeliveryState != "prepared" {
		t.Fatal("selection implicitly approved input")
	}
	wrong := input
	wrong.InputID = "00000000000000000000000000000000"
	if _, e := s.Approve(ctx, prepared.Item.ID, requestID(510), wrong); e == nil || e.Code != "execution_state_conflict" {
		t.Fatal("wrong input approved", e)
	}
	before, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, e := s.Approve(ctx, prepared.Item.ID, requestID(511), input)
	if e != nil || !r.Changed || r.Item.Revision != 2 || r.Item.DeliveryState != "approved" {
		t.Fatal("approval failed", e)
	}
	replay, e := s.Approve(ctx, prepared.Item.ID, requestID(511), input)
	if e != nil || !replay.Replayed || replay.Item != r.Item {
		t.Fatal("approval replay changed result", e)
	}
	input.Revision = r.Item.Revision
	noop, e := s.Approve(ctx, prepared.Item.ID, requestID(512), input)
	if e != nil || noop.Changed || noop.Item.Revision != 2 {
		t.Fatal("identical approval was not no-op", e)
	}
	if _, e := s.Cancel(ctx, prepared.Item.ID, requestID(503), RevisionInput{2}); e == nil || e.Code != "request_conflict" {
		t.Fatal("prepare UUID crossed action boundary", e)
	}
	if _, e := s.Cancel(ctx, prepared.Item.ID, requestID(513), RevisionInput{1}); e == nil || e.Code != "revision_conflict" {
		t.Fatal("stale cancellation committed", e)
	}
	canceled, e := s.Cancel(ctx, prepared.Item.ID, requestID(514), RevisionInput{2})
	if e != nil || canceled.Item.DeliveryState != "canceled" || canceled.Item.Revision != 3 {
		t.Fatal("cancel failed", e)
	}
	if _, err = os.Stat(filepath.Join(s.Root, s.path("value-execution-payloads", prepared.Item.ID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled snapshot survived", err)
	}
	input.Revision = 1
	replay, e = s.Approve(ctx, prepared.Item.ID, requestID(511), input)
	if e != nil || !replay.Replayed || replay.Item.DeliveryState != "approved" {
		t.Fatal("saved outcome was rewritten by cancellation", e)
	}
	v, e := s.Status(ctx, prepared.Item.ID, "")
	if e != nil || v.Item.DeliveryState != "canceled" {
		t.Fatal("replay reverted current state", e)
	}
	after, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.IdleExpiresAt == nil || after.IdleExpiresAt == nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("metadata controls extended idle expiry")
	}
	if err = c.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, e = s.Cancel(ctx, prepared.Item.ID, requestID(515), RevisionInput{3}); e == nil {
		t.Fatal("locked metadata mutation succeeded")
	}
}
func TestApproveCancelRaceAndUnregisterFence(t *testing.T) {
	s, _, r, input := approvedFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan string, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, e := s.Approve(ctx, r.Item.ID, requestID(520), input)
		if e == nil {
			results <- "approved"
		}
	}()
	go func() {
		defer wg.Done()
		_, e := s.Cancel(ctx, r.Item.ID, requestID(521), RevisionInput{1})
		if e == nil {
			results <- "canceled"
		}
	}()
	wg.Wait()
	close(results)
	wins := 0
	for range results {
		wins++
	}
	if wins != 1 {
		t.Fatal("concurrent metadata writers both committed", wins)
	}
	if _, e := s.Unregister(ctx, input.RunnerID, input.RunnerInstanceID, requestID(522)); e != nil {
		t.Fatal(e)
	}
	prepared, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "second-job", Selection: Selection{Keys: []string{}}}, requestID(523))
	if e != nil {
		t.Fatal(e)
	}
	input.Revision = 1
	input.InputID = prepared.Item.InputID
	input.ExternalExecutionID = prepared.Item.ExternalExecutionID
	if _, e = s.Approve(ctx, prepared.Item.ID, requestID(524), input); e == nil || e.Code != "runner_not_found" {
		t.Fatal("disabled runner admitted new approval", e)
	}
}
func TestExpiredApprovalCleansCiphertextWithoutBindingFailedRequest(t *testing.T) {
	s, _, r, input := approvedFixture(t)
	ctx := context.Background()
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	gate, err := s.lockCommit(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	record, err := s.readRecord(ctx, a, r.Item.ID)
	if err != nil {
		gate()
		t.Fatal(err)
	}
	record.Item.ExpiresAt = time.Now().Add(-time.Second)
	path := s.path("value-executions", r.Item.ID)
	b, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, record)
	if err == nil {
		err = maintenance.Replace(s.Root, map[string][]byte{path: b})
	}
	gate()
	if err != nil {
		t.Fatal(err)
	}
	if _, e := s.Approve(ctx, r.Item.ID, requestID(530), input); e == nil || e.Code != "execution_input_expired" {
		t.Fatal("expired input approved", e)
	}
	for _, path := range []string{s.path("value-execution-payloads", r.Item.ID), s.path("value-execution-requests", requestID(530))} {
		if _, err = os.Stat(filepath.Join(s.Root, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("expired ciphertext or failed receipt remained", err)
		}
	}
	v, e := s.Status(ctx, r.Item.ID, "")
	if e != nil || v.Item.DeliveryState != "expired" {
		t.Fatal("expiry cleanup lost state", e)
	}
}

func TestRotationInvalidatesApprovalButAllowsPendingCancellation(t *testing.T) {
	s, _, r, input := approvedFixture(t)
	ctx := context.Background()
	a, err := s.access(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	files := vault.FileStore{Root: s.Root, VaultID: a.vaultID, CatalogKey: a.master}
	if _, err = files.RotateProfile(ctx, s.Profile, a.master); err != nil {
		t.Fatal(err)
	}
	if _, e := s.Approve(ctx, r.Item.ID, requestID(540), input); e == nil || e.Code != vault.ErrInputChanged.Error() {
		t.Fatal("rotation did not invalidate old input", e)
	}
	if _, e := s.Cancel(ctx, r.Item.ID, requestID(541), RevisionInput{1}); e != nil {
		t.Fatal("old authority prevented safe pending deletion", e)
	}
	if _, err = os.Stat(filepath.Join(s.Root, s.path("value-execution-payloads", r.Item.ID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalidated pending ciphertext survived cancel", err)
	}
}
