package valuehandoff

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (s Store) hook(phase string) {
	if s.deliveryHook != nil {
		s.deliveryHook(phase)
	}
}

func beforeSendError(ctx context.Context, expires time.Time) *protocol.Error {
	if !time.Now().Before(expires) {
		return failure("execution_input_expired")
	}
	if e := protocol.ContextError(ctx.Err()); e != nil {
		return e
	}
	return failure("runner_protocol_mismatch")
}
func (s Store) attempted(ctx context.Context, r record) *protocol.Error {
	if r.Item.DeliveryID == "" {
		return nil
	}
	item, err := s.observed(ctx, r)
	if err != nil {
		return values.VaultError(ctx, err)
	}
	if item.DeliveryState == "delivering" {
		return failure("delivery_pending")
	}
	if item.DeliveryState == "unknown" {
		return failure("delivery_outcome_unknown")
	}
	return failure("delivery_already_started")
}
func (s Store) Deliver(ctx context.Context, id, requestID string, input RevisionInput) (result Result, condition *protocol.Error) {
	defer func() {
		if condition != nil && (condition.Code == "delivery_outcome_unknown" || condition.Code == "delivery_pending") {
			if condition.Details == nil {
				condition.Details = map[string]any{}
			}
			condition.Details["retry"] = "inspect_first"
			condition.Details["effects"] = "unknown"
			condition.Details["remedies"] = []protocol.Remedy{protocol.LocalAction([]string{"devtools", "values", "execution", "status", id, "--profile", s.Profile}, nil, "Inspect the original execution and receiver state. An uncertain delivery must not be sent again or replaced with a new prepare request.", "read").Remedy(protocol.InvocationContext{})}
		}
	}()
	if !idRE.MatchString(id) || !uuidRE.MatchString(requestID) || input.Revision == 0 {
		return Result{}, invalid()
	}
	a, err := s.access(ctx, true)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer a.master.Destroy()
	fp, err := fingerprint(a, "deliver", id, input)
	if err != nil {
		return Result{}, invalid()
	}
	read, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	saved, found, e := s.replay(ctx, a, requestID, fp)
	var r record
	if !found && e == nil {
		r, err = s.readRecord(ctx, a, id)
	}
	read()
	if found || e != nil {
		if found && (e == nil || e.Code == "delivery_outcome_unknown") {
			if err = s.cleanupReplay(ctx, a, saved.Item.ID); err != nil {
				return Result{}, values.VaultError(ctx, err)
			}
		}
		if e != nil && e.Code == "delivery_outcome_unknown" && idRE.MatchString(saved.Item.ID) {
			v, se := s.Status(ctx, saved.Item.ID, "")
			if se == nil && v.Item.DeliveryState == "delivering" {
				return saved, failure("delivery_pending")
			}
		}
		return saved, e
	}
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, failure("execution_not_found")
	}
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	if e = s.attempted(ctx, r); e != nil {
		return Result{}, e
	}
	if r.Item.DeliveryState != "approved" {
		return Result{}, failure("approval_required")
	}
	if !time.Now().Before(r.Item.ExpiresAt) {
		return s.control(ctx, id, requestID, "deliver", input, input.Revision, nil, func(current *access, pending *record) (bool, bool, *protocol.Error) {
			if pending.Item.DeliveryID != "" {
				return false, false, failure("delivery_already_started")
			}
			if pending.Item.DeliveryState != "approved" {
				return false, false, failure("execution_input_expired")
			}
			return false, false, s.pendingHeld(ctx, current, *pending)
		})
	}
	ctx, cancel := context.WithDeadline(ctx, r.Item.ExpiresAt)
	defer cancel()
	address, err := Address(s.Root, r.Item.RunnerID, r.Item.RunnerInstanceID)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	c, h, challenge, err := connectRunner(ctx, s.Root, address)
	if err != nil {
		return Result{}, beforeSendError(ctx, r.Item.ExpiresAt)
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	bind := bindFrame{1, "bind", address.RootID, address.VaultID, address.RunnerID, address.InstanceID, h.ServerChallenge, challenge, id, r.Item.InputID, r.Item.ExternalExecutionID}
	if err = writeFrame(c, bind, maxControlFrame); err != nil {
		return Result{}, beforeSendError(ctx, r.Item.ExpiresAt)
	}
	var ready readyFrame
	if err = readControl(c, &ready); err != nil || ready.Protocol != 1 || ready.Type != "ready" || ready.ServerChallenge != h.ServerChallenge || ready.ClientChallenge != challenge || ready.RedactionProtocol != 1 {
		return Result{}, beforeSendError(ctx, r.Item.ExpiresAt)
	}
	s.hook("ready")
	op, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(s.Root, "value-execution-locks", profilekey.Key(s.Profile), id+".lock"))
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	if !free {
		return Result{}, failure("delivery_pending")
	}
	defer op()
	launch, err := a.client.BeginDelivery(ctx, r.Ticket)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	slot, err := maintenance.LockShared(ctx, filepath.Join(s.Root, "vault", "key-slot-write.lock"))
	if err != nil {
		launch()
		return Result{}, values.VaultError(ctx, err)
	}
	leased := true
	release := func() {
		if leased {
			slot()
			launch()
			leased = false
		}
	}
	defer release()
	gate, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	gateHeld := true
	defer func() {
		if gateHeld {
			gate()
		}
	}()
	if saved, found, e := s.replay(ctx, a, requestID, fp); found || e != nil {
		return saved, e
	}
	r, err = s.readRecord(ctx, a, id)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	if r.Item.DeliveryID != "" {
		return Result{}, failure("delivery_already_started")
	}
	if r.Item.Revision != input.Revision {
		return Result{}, failure("revision_conflict")
	}
	if r.Item.DeliveryState != "approved" {
		return Result{}, failure("approval_required")
	}
	if r.Item.Revision > math.MaxUint64-2 {
		return Result{}, failure("execution_state_conflict")
	}
	identity, err := (vault.KeyFileStore{Directory: filepath.Join(s.Root, "vault")}).WriteIdentity()
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	if identity != a.identity {
		return Result{}, values.VaultError(ctx, vault.ErrInputChanged)
	}
	if e = s.pendingHeld(ctx, a, r); e != nil {
		return Result{}, e
	}
	registry, err := s.readRunner(ctx, a, r.Item.RunnerID)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	if !registry.Enabled || registry.InstanceID != r.Item.RunnerInstanceID {
		return Result{}, failure("runner_instance_conflict")
	}
	var snap struct {
		Values []storedValue `json:"values"`
	}
	if err = s.decode(ctx, a, s.path("value-execution-payloads", id), id, vault.MaterialExecutionSnapshot, &snap); err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer clear(snap.Values)
	delivery, err := vault.NewID()
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	payload := privatePayload{1, "payload", id, r.Item.InputID, delivery, r.Item.ExternalExecutionID, snap.Values, privateRedaction{1, patternsFor(snap.Values)}}
	defer payload.destroy()
	b, err := encodePayload(payload)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer clear(b)
	if !time.Now().Before(r.Item.ExpiresAt) || ctx.Err() != nil {
		return Result{}, beforeSendError(ctx, r.Item.ExpiresAt)
	}
	r.Item.DeliveryID = delivery
	r.Item.DeliveryState = "delivering"
	r.Item.ExecutionState = "unknown"
	r.Item.Revision++
	r.DeliveryRequestID = requestID
	intent := Result{Profile: s.Profile, Item: r.Item, Changed: true}
	files, err := s.recordFiles(ctx, a, r, requestID, fp, "deliver", intent, true)
	if err == nil {
		err = maintenance.Replace(s.Root, files)
	}
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	gate()
	gateHeld = false
	s.hook("intent")
	err = writeFrameBytes(c, b, maxPayloadFrame)
	if err == nil {
		s.hook("payload")
		var ack acceptedFrame
		err = readControl(c, &ack)
		if err == nil && (ack.Protocol != 1 || ack.Type != "accepted" || ack.ServerChallenge != h.ServerChallenge || ack.ClientChallenge != challenge || ack.ExecutionID != id || ack.InputID != r.Item.InputID || ack.DeliveryID != delivery || ack.RedactionProtocol != 1) {
			err = vault.ErrInvalid
		}
	}
	// A completed or aborted send drops physical admission leases immediately.
	// Durable intent remains if lock/rewrap wins before authenticated finalization.
	release()
	s.hook("finalize")
	finish, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancelFinish()
	result, finishErr := s.finishDelivery(finish, a, r, err == nil)
	if finishErr != nil || err != nil {
		return result, failure("delivery_outcome_unknown")
	}
	return result, nil
}

func (s Store) finishDelivery(ctx context.Context, a *access, r record, accepted bool) (Result, error) {
	release, err := s.lockCommit(ctx, a)
	if err != nil {
		return Result{}, err
	}
	defer release()
	current, err := s.readRecord(ctx, a, r.Item.ID)
	if err != nil {
		return Result{}, err
	}
	if current.Item.DeliveryID != r.Item.DeliveryID || current.Item.DeliveryState != "delivering" {
		return Result{}, vault.ErrIntegrity
	}
	var rr receipt
	path := s.path("value-execution-requests", r.DeliveryRequestID)
	if err = s.decode(ctx, a, path, r.DeliveryRequestID, vault.MaterialExecutionRequest, &rr); err != nil {
		return Result{}, err
	}
	if !rr.Pending || rr.Action != "deliver" || rr.Result.Item.DeliveryID != r.Item.DeliveryID {
		return Result{}, vault.ErrIntegrity
	}
	current.Item.Revision++
	current.Item.DeliveryState = "unknown"
	rr.ErrorCode = "delivery_outcome_unknown"
	if accepted {
		current.Item.DeliveryState = "delivered"
		rr.ErrorCode = ""
	}
	result := Result{Profile: s.Profile, Item: current.Item, Changed: true}
	rr.Pending = false
	rr.Result = result
	rp := s.path("value-executions", r.Item.ID)
	b, err := s.encode(ctx, a, rp, r.Item.ID, vault.MaterialExecutionRecord, current)
	if err != nil {
		return Result{}, err
	}
	rb, err := s.encode(ctx, a, path, r.DeliveryRequestID, vault.MaterialExecutionRequest, rr)
	if err != nil {
		return Result{}, err
	}
	if err = maintenance.Replace(s.Root, map[string][]byte{rp: b, path: rb, s.path("value-execution-payloads", r.Item.ID): nil}); err != nil {
		return Result{}, err
	}
	return result, nil
}
