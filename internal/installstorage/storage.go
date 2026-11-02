// Package installstorage observes the new executable's storage coordinator after
// executable publication. Data changes never participate in binary rollback.
package installstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

type Result struct {
	Status     string                 `json:"status"`
	Ready      bool                   `json:"ready"`
	Report     *storageupgrade.Report `json:"report,omitempty"`
	Diagnostic map[string]any         `json:"diagnostic,omitempty"`
}

func Failed(code string) Result {
	return Result{Status: "failed", Diagnostic: protocol.ObservationDiagnostic(protocol.NewError(code, "", 1, nil))}
}

// Run starts only the published candidate. Stdin is closed and both output
// streams are private, bounded buffers; no prompt or raw child error is relayed.
func Run(ctx context.Context, executable string) Result {
	command := exec.CommandContext(ctx, executable, "__install-storage")
	var output, diagnostic boundedOutput
	command.Stdout, command.Stderr = &output, &diagnostic
	if command.Run() != nil || output.truncated || diagnostic.Len() != 0 {
		return Failed("storage_upgrade_result_unavailable")
	}
	var result Result
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || !valid(result) {
		return Failed("storage_upgrade_result_invalid")
	}
	return result
}

func valid(result Result) bool {
	if result.Ready {
		return result.Status == "ready" && result.Report != nil && result.Report.Ready && result.Report.Scope == "storage"
	}
	return result.Status == "pending" || result.Status == "blocked" || result.Status == "failed"
}

type boundedOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *boundedOutput) Write(body []byte) (int, error) {
	n := len(body)
	remaining := (1 << 20) - b.Len()
	if len(body) > remaining {
		body = body[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(body)
	return n, nil
}
