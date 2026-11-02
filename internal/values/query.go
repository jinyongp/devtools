package values

import (
	"context"
	"errors"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// ReadForQuery reads the same metadata/public view as Read while allowing a
// clean, existing store to be opened without write access. Mutation preparation
// and execution snapshots retain their existing storage/session contracts.
func (s ModernStore) ReadForQuery(ctx context.Context) (*State, *protocol.Error) {
	if !project.ValidProfile(s.Profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	release, err := maintenance.AcquireQuery(ctx, s.root())
	if err != nil {
		return nil, s.queryError(ctx, err)
	}
	defer release()
	return s.ReadHeld(ctx)
}

func (s ModernStore) queryError(ctx context.Context, cause error) *protocol.Error {
	return QueryError(ctx, cause)
}

// QueryError retains required preparation and its effects for read consumers.
func QueryError(ctx context.Context, cause error) *protocol.Error {
	failure := VaultError(ctx, cause)
	if failure.Code == "canceled" || failure.Code == "deadline_exceeded" {
		return failure
	}
	var preparation *maintenance.QueryPreparationError
	if !errors.As(cause, &preparation) {
		return failure
	}
	if failure.Details == nil {
		failure.Details = map[string]any{}
	}
	failure.Message = "Storage initialization or recovery is required before this query can proceed."
	failure.Details["state"] = preparation.State
	failure.Details["effects"] = "unknown"
	if preparation.Passive {
		failure.Details["effects"] = "none"
	}
	failure.Details["retry"] = "after_dependency_change"
	failure.Details["remedies"] = []protocol.Remedy{{
		Relation: protocol.SameStoreAction,
		Argv:     []string{"devtools", "doctor", "--storage", "--fix"},
		Effect:   "value_write", RequiredInputs: []string{},
		Message: "Prepare or recover the existing storage in a context with write access, then retry the query.",
	}}
	return failure
}
