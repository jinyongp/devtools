// Package protocol defines the versioned machine-facing response contract.
package protocol

import (
	"encoding/json"
	"io"
)

const Version = 1

// Error contains a stable code suitable for programmatic branching.
type Error struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
	ExitCode int            `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func NewError(code, message string, exitCode int, details map[string]any) *Error {
	return &Error{Code: code, Message: message, ExitCode: exitCode, Details: details}
}

type response struct {
	SchemaVersion int    `json:"schema_version"`
	OK            bool   `json:"ok"`
	Data          any    `json:"data,omitempty"`
	Error         *Error `json:"error,omitempty"`
}

func Success(w io.Writer, data any) error {
	return json.NewEncoder(w).Encode(response{SchemaVersion: Version, OK: true, Data: data})
}

func Failure(w io.Writer, err *Error) error {
	return json.NewEncoder(w).Encode(response{SchemaVersion: Version, OK: false, Error: err})
}
