package protocol

import (
	"context"
	"errors"
)

// ContextError preserves the distinction between an explicit cancellation and
// an elapsed preparation deadline. Other errors are left to their owner.
func ContextError(err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return NewError("deadline_exceeded", "The operation deadline elapsed.", 3, nil)
	}
	if errors.Is(err, context.Canceled) {
		return NewError("canceled", "Execution canceled.", 130, nil)
	}
	return nil
}
