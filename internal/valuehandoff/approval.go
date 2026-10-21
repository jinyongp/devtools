package valuehandoff

import (
	"context"
	"errors"
	"math"
	"os"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

type RevisionInput struct {
	Revision uint64 `json:"revision"`
}
type ApprovalInput struct {
	Revision            uint64 `json:"revision"`
	RunnerID            string `json:"runner_id"`
	RunnerInstanceID    string `json:"runner_instance_id"`
	ExternalExecutionID string `json:"external_execution_id"`
	InputID             string `json:"input_id"`
}

// control uses only a warm cached key. Its callback runs under the final
// maintenance gate; preflight runs before any operation/disk gate is held.
func (s Store) control(ctx context.Context, id, requestID, action string, input any, revision uint64, preflight func(context.Context) error, change func(*access, *record) (bool, bool, *protocol.Error)) (Result, *protocol.Error) {
	return s.controlFiles(ctx, id, requestID, action, input, revision, preflight, change, nil)
}

func (s Store) controlFiles(ctx context.Context, id, requestID, action string, input any, revision uint64, preflight func(context.Context) error, change func(*access, *record) (bool, bool, *protocol.Error), extra func(context.Context, *access, record, Result, map[string][]byte) error) (Result, *protocol.Error) {
	if !idRE.MatchString(id) || !uuidRE.MatchString(requestID) || revision == 0 {
		return Result{}, invalid()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, err := s.access(ctx, false)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer a.master.Destroy()
	fp, err := fingerprint(a, action, id, input)
	if err != nil {
		return Result{}, invalid()
	}
	read, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	saved, found, e := s.replay(ctx, a, requestID, fp)
	read()
	if found || e != nil {
		if found && e == nil {
			if err = s.cleanupReplay(ctx, a, saved.Item.ID); err != nil {
				return Result{}, values.VaultError(ctx, err)
			}
		}
		return saved, e
	}
	if preflight != nil {
		if err = preflight(ctx); err != nil {
			if ctx.Err() != nil {
				return Result{}, values.VaultError(ctx, ctx.Err())
			}
			return Result{}, failure("runner_protocol_mismatch")
		}
	}
	op, err := s.operationLease(ctx, id)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer op()
	commit, err := s.lockCommit(ctx, a)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer commit()
	if saved, found, e := s.replay(ctx, a, requestID, fp); found || e != nil {
		return saved, e
	}
	r, err := s.readRecord(ctx, a, id)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, failure("execution_not_found")
	}
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	if r.Item.Revision != revision {
		return Result{}, failure("revision_conflict")
	}
	if r.Item.Revision == math.MaxUint64 {
		return Result{}, failure("execution_state_conflict")
	}
	changed, erase, e := change(a, &r)
	if e != nil {
		return Result{}, e
	}
	if changed {
		r.Item.Revision++
	}
	result := Result{Profile: s.Profile, Item: r.Item, Changed: changed}
	files, err := s.recordFiles(ctx, a, r, requestID, fp, action, result, false)
	if err == nil && erase {
		files[s.path("value-execution-payloads", id)] = nil
	}
	if err == nil && extra != nil {
		err = extra(ctx, a, r, result, files)
	}
	if err == nil {
		err = maintenance.Replace(s.Root, files)
	}
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	return result, nil
}
func (s Store) pendingHeld(ctx context.Context, a *access, r record) *protocol.Error {
	if !time.Now().Before(r.Item.ExpiresAt) {
		if (r.Item.DeliveryState == "prepared" || r.Item.DeliveryState == "approved") && r.Item.Revision < math.MaxUint64 {
			r.Item.DeliveryState = "expired"
			r.Item.Revision++
			path := s.path("value-executions", r.Item.ID)
			b, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, r)
			if err == nil {
				err = maintenance.Replace(s.Root, map[string][]byte{path: b, s.path("value-execution-payloads", r.Item.ID): nil})
			}
			if err != nil {
				return values.VaultError(ctx, err)
			}
		}
		return failure("execution_input_expired")
	}
	if r.Ticket.SessionID != a.client.Endpoint.SessionID {
		return values.VaultError(ctx, vault.ErrInputChanged)
	}
	if err := s.validateFrozenHeld(ctx, a, r); err != nil {
		return values.VaultError(ctx, err)
	}
	return nil
}
func (s Store) Approve(ctx context.Context, id, requestID string, input ApprovalInput) (Result, *protocol.Error) {
	if !uuidRE.MatchString(input.RunnerID) || !uuidRE.MatchString(input.RunnerInstanceID) || !idRE.MatchString(input.InputID) || !opaque(input.ExternalExecutionID) {
		return Result{}, invalid()
	}
	preflight := func(ctx context.Context) error {
		address, err := Address(s.Root, input.RunnerID, input.RunnerInstanceID)
		if err != nil {
			return err
		}
		return probeRunner(ctx, s.Root, address)
	}
	return s.control(ctx, id, requestID, "approve", input, input.Revision, preflight, func(a *access, r *record) (bool, bool, *protocol.Error) {
		if r.Item.InputID != input.InputID || r.Item.ExternalExecutionID != input.ExternalExecutionID {
			return false, false, failure("execution_state_conflict")
		}
		if r.Item.DeliveryState != "prepared" && r.Item.DeliveryState != "approved" {
			return false, false, failure("execution_state_conflict")
		}
		if e := s.pendingHeld(ctx, a, *r); e != nil {
			return false, false, e
		}
		runner, err := s.readRunner(ctx, a, input.RunnerID)
		if errors.Is(err, os.ErrNotExist) || err == nil && !runner.Enabled {
			return false, false, failure("runner_not_found")
		}
		if err != nil {
			return false, false, values.VaultError(ctx, err)
		}
		if runner.InstanceID != input.RunnerInstanceID {
			return false, false, failure("runner_instance_conflict")
		}
		if r.Item.DeliveryState == "approved" {
			if r.Item.RunnerID != input.RunnerID || r.Item.RunnerInstanceID != input.RunnerInstanceID {
				return false, false, failure("runner_instance_conflict")
			}
			return false, false, nil
		}
		r.Item.DeliveryState = "approved"
		r.Item.RunnerID = input.RunnerID
		r.Item.RunnerInstanceID = input.RunnerInstanceID
		return true, false, nil
	})
}
func (s Store) Cancel(ctx context.Context, id, requestID string, input RevisionInput) (Result, *protocol.Error) {
	return s.control(ctx, id, requestID, "cancel", input, input.Revision, nil, func(a *access, r *record) (bool, bool, *protocol.Error) {
		if r.Item.DeliveryID != "" || r.Item.DeliveryState == "delivering" || r.Item.DeliveryState == "delivered" || r.Item.DeliveryState == "unknown" {
			return false, false, failure("delivery_already_started")
		}
		if r.Item.DeliveryState == "canceled" {
			return false, true, nil
		}
		if r.Item.DeliveryState != "prepared" && r.Item.DeliveryState != "approved" {
			return false, false, failure("execution_state_conflict")
		}
		// Cancellation removes pending authority. Its current cached-key/write
		// identity fence is sufficient even if the old input was invalidated by
		// rotation or a replaced session; it must never refresh that authority.
		r.Item.DeliveryState = "canceled"
		return true, true, nil
	})
}
