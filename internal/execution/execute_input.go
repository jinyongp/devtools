package execution

import (
	"context"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
	"io"
	"time"
)

func (p Prepared) executeInput(ctx context.Context, runner process.Runner, in io.Reader, out, diagnostic io.Writer) (code int, failure *protocol.Error) {
	if e := p.Input.Validate(time.Now()); e != nil {
		return 0, e
	}
	input, e := p.Input.Open()
	if e != nil {
		return 0, e
	}

	defer input.Destroy()
	if !p.Input.consumed.CompareAndSwap(false, true) {
		return 0, snapshotError("input_snapshot_consumed")
	}

	streamChanged := func(reason string, writer io.Writer) bool {
		return (reason == "tty" && !process.OutputTTY(writer)) || (reason == "non_tty" && process.OutputTTY(writer))
	}
	if streamChanged(p.Input.metadata.Output.StdoutReason, out) || streamChanged(p.Input.metadata.Output.StderrReason, diagnostic) {
		return 0, protocol.NewError("output_stream_changed", "Output stream changed after planning; prepare a new execution.", 3, nil)
	}
	protected, outputErr := redact.Protect(ctx, input.Patterns, p.Input.metadata.Output, out, diagnostic)
	if outputErr != nil {
		return 0, OutputProtectionFailure(outputErr, p.Command.Diagnostic(p.mode))
	}
	ctx = protected.Context
	defer func() {
		if protected.Close(ctx.Err() != nil || failure != nil) != nil {
			outputFailure := protocol.NewError("output_protection_failed", "Protected output could not be delivered safely.", 1, nil)
			protocol.PreserveRecovery(outputFailure, failure)
			failure = outputFailure
		}
	}()
	var admission *vault.LaunchAdmission
	var admissionErr error
	if ticket := p.Input.metadata.Values.Public; ticket != nil {
		admission, admissionErr = vault.BeginPublicLaunch(ctx, input.VaultDirectory, p.Input.metadata.InputID, *ticket)
	} else if ticket := p.Input.metadata.Values.Launch; ticket != nil {
		client, err := vault.OpenSessionClient(input.VaultDirectory)
		if err != nil {
			return 0, values.VaultError(ctx, err)
		}
		admission, admissionErr = client.BeginLaunch(ctx, p.Input.metadata.InputID, *ticket)
	}
	if admissionErr != nil {
		return 0, values.VaultError(ctx, admissionErr)
	}
	if admission != nil {
		completed := false
		defer func() {
			if !completed {
				admission.Finish(context.Background(), "canceled")
			}
		}()
		beforeAdmission := ctx
		ctx = process.WithStarted(ctx, func() *protocol.Error {
			completed = true
			observed := process.NotifyStarted(beforeAdmission)
			err := admission.Finish(context.Background(), "spawned")
			if observed != nil {
				return observed
			}
			if err != nil {
				return values.VaultError(ctx, err)
			}
			return nil
		})
	}
	ctx = process.WithExecutablePath(process.WithReadyProbe(ctx, input.Ready), input.Executable)
	if runner != nil {
		if e := process.ValidateManaged(input.Args, input.Directory, input.Environment); e != nil {
			return 0, e
		}
		ctx = process.WithRunner(ctx, runner)
	}
	return process.Execute(ctx, input.Args, input.Directory, input.Environment, in, protected.Out, protected.Err)
}
