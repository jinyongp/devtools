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

// WithStoragePreparation keeps query preparation separate from schema reads.
// Passive inspection observes the requirement without attempting any writes.
func WithStoragePreparation(e *Error, state string, passive bool) *Error {
	if e.Code == "canceled" || e.Code == "deadline_exceeded" {
		return e
	}
	copy := *e
	copy.Details = map[string]any{}
	for key, value := range e.Details {
		copy.Details[key] = value
	}
	copy.Message = "Storage initialization or recovery is required before this query can proceed."
	copy.Details["state"] = state
	copy.Details["effects"] = "unknown"
	if passive {
		copy.Details["effects"] = "none"
	}
	copy.Details["retry"] = "after_dependency_change"
	copy.Details["remedies"] = []Remedy{{Relation: SameStoreAction, Argv: []string{"devtools", "doctor", "--storage", "--fix"}, Effect: "value_write", RequiredInputs: []string{}, Message: "Prepare or recover the existing storage in a context with write access, then retry the query."}}
	return &copy
}
