package protocol

import "github.com/jinyongp/devtools/internal/storageerr"

// ObservationDiagnostic projects only typed causes for nested failed reads.
// Error messages, paths, value payloads, arbitrary facts and inner errors are
// deliberately absent from this public observation contract.
func ObservationDiagnostic(e *Error) map[string]any {
	if e == nil {
		return nil
	}
	details := storageerr.SafeDetails(e.Details)
	if details == nil {
		details = map[string]any{}
	}
	allowed := map[string]map[string]bool{
		"state":   {"initialization_required": true, "recovery_required": true, "migration_required": true},
		"effects": {"none": true, "committed": true, "partial": true, "unknown": true},
		"retry":   {"never": true, "after_input_change": true, "after_refresh": true, "after_dependency_change": true, "inspect_first": true, "same_request": true},
		"phase":   {"parse": true, "resolve": true, "validate": true, "prepare": true, "execute": true, "commit": true, "observe": true, "cleanup": true},
	}
	for key, values := range allowed {
		if value, ok := e.Details[key].(string); ok && values[value] {
			details[key] = value
		}
	}
	return map[string]any{"code": e.Code, "details": details}
}
