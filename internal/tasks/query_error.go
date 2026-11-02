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
	return protocol.WithStoragePreparation(failure, preparation.State, preparation.Passive)
}
