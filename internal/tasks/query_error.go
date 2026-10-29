package tasks

import (
	"context"
	"errors"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) queryGateError(ctx context.Context, cause error) *protocol.Error {
	if e := protocol.ContextError(cause); e != nil {
		return e
	}
	failure := gateError(ctx, cause)
	if failure.Code == "canceled" || failure.Code == "deadline_exceeded" {
		return failure
	}
	var preparation *maintenance.QueryPreparationError
	if !errors.As(cause, &preparation) {
		return failure
	}
	failure.Message = "Storage initialization or recovery is required before this query can proceed."
	failure.Details["state"] = preparation.State
	failure.Details["effects"] = "unknown"
	failure.Details["retry"] = "after_dependency_change"
	failure.Details["remedies"] = []protocol.Remedy{{
		Relation: protocol.SameStoreAction,
		Argv:     []string{"devtools", "task", "next", "--profile", s.Profile},
		Effect:   "value_write", RequiredInputs: []string{},
		Message: "Run task next with write access to the existing store to initialize or recover storage, then retry the original query with the same options.",
	}}
	return failure
}
