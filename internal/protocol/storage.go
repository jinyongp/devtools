package protocol

import (
	"errors"
	"github.com/jinyongp/devtools/internal/storageerr"
)

// WithStorageDetails preserves the owner's code, exit status, and domain details.
// Low-level failures describe the original operation; absent OS errors stay absent.
func WithStorageDetails(e *Error, domain, operation, resource string, cause error) *Error {
	if contextError := ContextError(cause); contextError != nil {
		return contextError
	}
	var inner *Error
	if errors.As(cause, &inner) && (inner.Code == "canceled" || inner.Code == "deadline_exceeded") {
		return inner
	}
	copy := *e
	copy.Details = make(map[string]any, len(e.Details)+6)
	for key, value := range e.Details {
		copy.Details[key] = value
	}
	for key, value := range storageerr.Details(cause, domain, operation, resource) {
		copy.Details[key] = value
	}
	if copy.Details["phase"] == nil {
		copy.Details["phase"] = "prepare"
	}
	if inner != nil {
		for key, value := range storageerr.SafeDetails(inner.Details) {
			copy.Details[key] = value
		}
	}
	return &copy
}
