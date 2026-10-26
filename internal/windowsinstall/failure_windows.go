package windowsinstall

import (
	"errors"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Failure struct {
	Stage, Effects string
	Cause          error
}

func (e *Failure) Error() string { return "installation failed at " + e.Stage }
func (e *Failure) Unwrap() error { return e.Cause }

// Diagnostic retains typed installer/OS facts and keeps filesystem paths and
// native exception text inside the error chain.
func Diagnostic(err error, operation string) *protocol.Error {
	stage, effects := "input", "none"
	var failure *Failure
	if errors.As(err, &failure) {
		stage, effects = failure.Stage, failure.Effects
	}
	e := protocol.WithStorageDetails(protocol.NewError("install_failed", "Installation could not complete. Inspect the stage, installation ownership/lock and any retained recovery journal before retrying.", 1, nil), "installer", stage, "installation", err)
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details["stage"] = stage
	e.Details["effects"] = effects
	e.Details["retry"] = "inspect_first"
	if reason, ok := e.Details["reason"].(string); !ok || reason == "unknown" {
		e.Details["reason"] = "stage_failed"
		if e.Code == "canceled" || e.Code == "deadline_exceeded" {
			e.Details["reason"] = e.Code
		}
	}
	return protocol.Public(e, operation)
}
