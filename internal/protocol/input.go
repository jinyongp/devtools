package protocol

// InputError identifies a caller-supplied input without exposing its path or bytes.
func InputError(role, operation string, cause error) *Error {
	e := WithStorageDetails(NewError("io_error", "Cannot read the selected input. Check that it exists and is readable.", 1, nil), "input", operation, role, cause)
	if e.Code == "canceled" || e.Code == "deadline_exceeded" {
		return e
	}
	e.Details["phase"] = "prepare"
	e.Details["input_role"] = role
	e.Details["target"] = map[string]any{"kind": "input", "name": role}
	return e
}
