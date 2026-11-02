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
	return protocol.WithStoragePreparation(failure, preparation.State, preparation.Passive)
}
