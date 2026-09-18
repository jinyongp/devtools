// Package protocol defines the versioned machine-facing response contract.
package protocol

import (
	"encoding/json"
	"io"
)

const (
	// EnvelopeVersion versions the common success/error response wrapper.
	EnvelopeVersion = 1
	// ProtocolVersion versions the public CLI machine contract, including command data shapes.
	ProtocolVersion = 3
)

// Error contains a stable code suitable for programmatic branching.
type Remedy struct {
	Argv           []string `json:"argv"`
	RequiredInputs []string `json:"required_inputs"`
	Message        string   `json:"message"`
}

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

type successResponse struct {
	SchemaVersion int  `json:"schema_version"`
	OK            bool `json:"ok"`
	Data          any  `json:"data"`
}

type failureResponse struct {
	SchemaVersion int    `json:"schema_version"`
	OK            bool   `json:"ok"`
	Error         *Error `json:"error"`
}

func Success(w io.Writer, data any) error {
	return json.NewEncoder(w).Encode(successResponse{SchemaVersion: EnvelopeVersion, OK: true, Data: data})
}

func Failure(w io.Writer, err *Error) error {
	return json.NewEncoder(w).Encode(failureResponse{SchemaVersion: EnvelopeVersion, OK: false, Error: err})
}
