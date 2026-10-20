// Package storageerr preserves private storage failure metadata without exposing
// paths, document contents, or the operating system's free-form error message.
package storageerr

import (
	"errors"
	"io/fs"
	"runtime"
	"syscall"
)

var ErrCorrupt = errors.New("stored document failed validation")

// Error describes the first failed storage operation. Cause remains available
// to errors.Is/As; callers must serialize Details, never Cause.Error().
type Error struct {
	Operation string
	Resource  string
	Reason    string
	Cause     error
}

func (e *Error) Error() string { return "private storage operation failed: " + e.Operation }
func (e *Error) Unwrap() error { return e.Cause }

// Wrap preserves the innermost operation rather than relabeling an ancestor
// creation failure as a lock or document failure at every higher boundary.
func Wrap(operation, resource, reason string, cause error) error {
	if cause == nil {
		return nil
	}
	var existing *Error
	if errors.As(cause, &existing) {
		return cause
	}
	return &Error{Operation: operation, Resource: resource, Reason: reason, Cause: cause}
}

// Decode marks malformed documents while retaining real read/permission errors.
func Decode(operation, resource string, cause error) error {
	var code syscall.Errno
	var path *fs.PathError
	reason := "invalid_storage"
	if errors.As(cause, &code) || errors.As(cause, &path) || errors.Is(cause, fs.ErrPermission) || errors.Is(cause, fs.ErrNotExist) {
		reason = ""
	}
	return Wrap(operation, resource, reason, cause)
}

func Details(cause error, domain, operation, resource string) map[string]any {
	reason := "unknown"
	osCause := cause
	var failure *Error
	if errors.As(cause, &failure) {
		osCause = failure.Cause
		operation = failure.Operation
		switch failure.Resource {
		case "private_storage", "private_directory", "private_file", "private_document":
			// The domain knows the role of a generated filename or private object.
		default:
			resource = failure.Resource
		}
		if failure.Reason != "" {
			reason = failure.Reason
		}
	}
	if reason == "unknown" {
		switch {
		case errors.Is(osCause, ErrCorrupt):
			reason = "invalid_storage"
		case errors.Is(osCause, fs.ErrPermission):
			reason = "access_denied"
		case errors.Is(osCause, fs.ErrNotExist):
			reason = "not_found"
		case errors.Is(osCause, fs.ErrExist):
			reason = "already_exists"
		default:
			reason = osReason(osCause)
		}
	}
	details := map[string]any{"domain": domain, "operation": operation, "resource": resource, "reason": reason}
	var code syscall.Errno
	if errors.As(osCause, &code) && code != 0 {
		details["os_error"] = uint64(code)
		details["os_error_domain"] = "errno"
		if runtime.GOOS == "windows" {
			details["os_error_domain"] = "win32"
		}
	}
	return details
}

// SafeDetails copies only the storage contract, even when a domain error also
// carries remedies, partial results, or other sensitive application metadata.
func SafeDetails(details map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"domain", "operation", "resource", "reason", "os_error_domain"} {
		if value, ok := details[key].(string); ok && safeLabel(value) {
			out[key] = value
		}
	}
	switch value := details["os_error"].(type) {
	case uint64:
		if value > 0 && value <= 0xffffffff {
			out["os_error"] = value
		}
	case int:
		if value > 0 && uint64(value) <= 0xffffffff {
			out["os_error"] = value
		}
	case float64:
		if value > 0 && value <= 0xffffffff && value == float64(uint64(value)) {
			out["os_error"] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func safeLabel(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, c := range value {
		if c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
