package cli

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/vault"
)

// ServeValueRestart is a bounded single-operation coordinator. It is detached
// from the old supervisor, so stopping that supervisor cannot cancel the launch.
// It is not part of the key-session service and receives no plaintext payload.
func ServeValueRestart(ctx context.Context, data, body string) error {
	if len(body) > 8192 {
		return errors.New("invalid value apply request")
	}
	var request services.ValueApplyStatus
	if vault.StrictDecode([]byte(body), &request) != nil || request.State != "requested" || request.Goal == nil {
		return errors.New("invalid value apply request")
	}
	store := services.Store{Data: data}
	persisted, e := store.ReadValueApply(request.ExecutionID)
	if e != nil || persisted.RequestID != request.RequestID || persisted.Goal == nil || *persisted.Goal != *request.Goal {
		return errors.New("value apply identity mismatch")
	}
	if persisted.State != "requested" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	finish := func(next, reason string) error { return store.CompleteValueApply(request, next, reason) }
	original, failure := store.Status(ctx, request.ExecutionID)
	if failure != nil {
		return finish("", failure.Code)
	}
	app := New("dev", "unknown")
	app.isolatedRoot = original.IsolatedRoot
	app.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(data, "profiles"), nil }
	store.PrepareInput = app.prepareManagedInput
	store.IsolatedRoot = original.IsolatedRoot
	q := services.Request{Action: "restart", ID: original.ID, RequestID: request.RequestID, AutoGoal: request.Goal}
	if original.ProxyMode {
		p, failure := project.Resolve(original.Directory, "")
		if failure != nil {
			return finish("", failure.Code)
		}
		// Existing network preparation remains noninteractive: no implicit privilege
		// grant, credential prompt, key backend installation or route policy change.
		failure = app.prepareManagedNetwork(ctx, p, &q, &original, network.PrepareAuto, IO{In: nil, Out: io.Discard, Err: io.Discard})
		if failure != nil {
			return finish("", failure.Code)
		}
	}
	result, failure := store.Apply(ctx, q)
	if failure != nil {
		return finish(result.Item.ID, failure.Code)
	}
	// Apply confirms the mutation/record even when the child failed to start.
	// Delivery requires an observed start, independently of the mutation result.
	if result.Item.StartedAt == nil {
		if result.Item.State == "unknown" || result.Item.State == "interrupted" {
			return finish(result.Item.ID, "launch_outcome_unknown")
		}
		return finish(result.Item.ID, "execution_failed")
	}
	if result.Item.EndedAt != nil || result.Item.State != "running" {
		return finish(result.Item.ID, "process_not_running")
	}
	if result.Item.ReadyConfigured {
		_, failure = store.Wait(ctx, result.Item.ID, 30*time.Second)
		if failure != nil {
			return finish(result.Item.ID, failure.Code)
		}
		return finish(result.Item.ID, "readiness_passed")
	}
	return finish(result.Item.ID, "values_delivered")
}
