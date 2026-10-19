package services

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

// The anonymous input/output pipes are inherited only by the chosen same-binary
// launcher. Neither actual input nor transport keys appear in argv, environment,
// records or disk. EOF before commit aborts; retry never chooses newer input.
const (
	launchBound    byte = 1
	launchAdmitted byte = 2
	launchCommit   byte = 3
)

type launcherInputChannel struct {
	write       *os.File
	read        *os.File
	inputRead   *os.File
	outputWrite *os.File
	stop        func() bool
}

func newLauncherInputChannel(ctx context.Context, cmd *exec.Cmd) (*launcherInputChannel, error) {
	input, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	read, output, err := os.Pipe()
	if err != nil {
		input.Close()
		write.Close()
		return nil, err
	}
	channel := &launcherInputChannel{write: write, read: read, inputRead: input, outputWrite: output}
	channel.stop = context.AfterFunc(ctx, func() { write.Close(); read.Close() })
	cmd.Stdin = input
	cmd.Stdout = output
	cmd.Stderr = nil
	return channel, nil
}
func (c *launcherInputChannel) inherited() { c.inputRead.Close(); c.outputWrite.Close() }
func (c *launcherInputChannel) close() {
	if c == nil {
		return
	}
	c.stop()
	c.write.Close()
	c.read.Close()
	c.inputRead.Close()
	c.outputWrite.Close()
}
func launcherFailureCode(code string) string {
	switch code {
	case "input_changed", "redaction_unavailable", "vault_locked", "key_missing", "integrity_failed", "recovery_required", "lock_pending", "input_snapshot_expired", "value_expired", "invalid_input_snapshot", "launch_canceled", "key_session_protocol_mismatch", "vault_recovery_required", "key_backend_unavailable", "key_access_denied", "key_interaction_required", "key_backend_locked", "invalid_vault_format", "vault_input_too_large":
		return code
	default:
		return "launcher_unavailable"
	}
}

// ReportLauncherFailure sends only a closed, value-free error code on the
// single-operation control pipe. stderr remains unavailable to the caller.
func ReportLauncherFailure(out io.Writer, e *protocol.Error) {
	code := launcherFailureCode(e.Code)
	frame := append([]byte{0, byte(len(code))}, []byte(code)...)
	out.Write(frame)
}
func (c *launcherInputChannel) expect(value byte) *protocol.Error {
	var b [1]byte
	if _, err := io.ReadFull(c.read, b[:]); err != nil {
		return failure("launcher_unavailable")
	}
	if b[0] == value {
		return nil
	}
	if b[0] != 0 {
		return failure("launcher_unavailable")
	}
	if _, err := io.ReadFull(c.read, b[:]); err != nil || b[0] == 0 || b[0] > 64 {
		return failure("launcher_unavailable")
	}
	code := make([]byte, int(b[0]))
	if _, err := io.ReadFull(c.read, code); err != nil {
		return failure("launcher_unavailable")
	}
	return failure(launcherFailureCode(string(code)))
}
func (c *launcherInputChannel) bind(payload *execution.InputPayload) (string, *protocol.Error) {
	encoded, e := payload.EncodeForLauncher()
	if e != nil {
		return "", e
	}
	defer encoded.Destroy()
	b := encoded.CopyBytes()
	defer clear(b)
	snapshotID := payload.Metadata().InputID
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(b)))
	if _, err := c.write.Write(size[:]); err != nil {
		return "", failure("launcher_unavailable")
	}
	if _, err := c.write.Write(b); err != nil {
		return "", failure("launcher_unavailable")
	}
	if err := c.expect(launchBound); err != nil {
		return "", err
	}
	return snapshotID, nil
}
func (c *launcherInputChannel) admit() *protocol.Error {
	if _, err := c.write.Write([]byte{launchAdmitted}); err != nil {
		return failure("launcher_unavailable")
	}
	if err := c.expect(launchAdmitted); err != nil {
		return err
	}
	return nil
}
func (c *launcherInputChannel) commit() *protocol.Error {
	if _, err := c.write.Write([]byte{launchCommit}); err != nil {
		return failure("launch_outcome_unknown")
	}
	return nil
}

// BoundInput owns an immutable payload and, after Admit, a durable launch lease.
// One launcher handles exactly one request. Complete at the real spawn boundary.
type BoundInput struct {
	Streams   *redact.Streams
	finished  bool
	Payload   *execution.InputPayload
	admission *vault.LaunchAdmission
}

func ReceiveLauncherInput(ctx context.Context, in io.ReadCloser, out io.WriteCloser, record Record, data string, stdout, stderr io.Writer) (*BoundInput, *protocol.Error) {
	runContext := ctx
	ctx, cancel := context.WithTimeout(ctx, execution.DefaultPreparationTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { in.Close(); out.Close() })
	defer stop()
	var size [4]byte
	if _, err := io.ReadFull(in, size[:]); err != nil {
		return nil, failure("launcher_input_unavailable")
	}
	n := binary.BigEndian.Uint32(size[:])
	if n == 0 || n > vault.MaxFrameBytes {
		return nil, failure("invalid_input_snapshot")
	}
	b := make([]byte, n)
	defer clear(b)
	if _, err := io.ReadFull(in, b); err != nil {
		return nil, failure("launcher_input_unavailable")
	}
	encoded := vault.NewMaterial(b)
	payload, e := execution.DecodeLauncherInput(encoded)
	encoded.Destroy()
	if e != nil {
		return nil, e
	}
	bound := &BoundInput{Payload: payload}
	success := false
	defer func() {
		if !success {
			bound.Close()
		}
	}()
	metadata := payload.Metadata()
	input, e := payload.Open()
	if e != nil {
		return nil, e
	}
	matches := record.Output != nil && *record.Output == metadata.Output && (record.Capture == (metadata.Output.StdoutReason == "capture")) && metadata.InputID == record.InputSnapshotID && metadata.Profile == record.Profile && metadata.Command == record.Command && metadata.Env == record.Env && input.Directory == record.Directory
	if matches {
		var outputErr error
		bound.Streams, outputErr = redact.Protect(runContext, input.Patterns, metadata.Output, stdout, stderr)
		if outputErr != nil {
			input.Destroy()
			return nil, protocol.NewError("redaction_unavailable", "Cannot initialize the requested output protection.", 3, nil)
		}
	}
	input.Destroy()
	if !matches {
		return nil, failure("invalid_input_snapshot")
	}
	if e = payload.Validate(time.Now()); e != nil {
		return nil, e
	}
	if _, err := out.Write([]byte{launchBound}); err != nil {
		return nil, failure("launcher_unavailable")
	}
	var signal [1]byte
	if _, err := io.ReadFull(in, signal[:]); err != nil || signal[0] != launchAdmitted {
		return nil, failure("launch_canceled")
	}
	if e = payload.Validate(time.Now()); e != nil {
		return nil, e
	}
	if ticket := metadata.Values.Public; ticket != nil {
		admission, err := vault.BeginPublicLaunch(ctx, filepath.Join(data, "vault"), metadata.InputID, *ticket)
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
		bound.admission = admission
	} else if ticket := metadata.Values.Launch; ticket != nil {
		client, err := vault.OpenSessionClient(filepath.Join(data, "vault"))
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
		bound.admission, err = client.BeginLaunch(ctx, metadata.InputID, *ticket)
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
	}
	if _, err := out.Write([]byte{launchAdmitted}); err != nil {
		return nil, failure("launch_canceled")
	}
	if _, err := io.ReadFull(in, signal[:]); err != nil || signal[0] != launchCommit {
		return nil, failure("launch_canceled")
	}
	success = true
	return bound, nil
}
func (b *BoundInput) Complete(outcome string) *protocol.Error {
	if b.admission == nil {
		return nil
	}
	admission := b.admission
	b.admission = nil
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if admission.Finish(ctx, outcome) != nil {
		return failure("launch_outcome_unknown")
	}
	return nil
}
func (b *BoundInput) FinishOutput(abort bool) *protocol.Error {
	b.finished = true
	if b.Streams != nil && b.Streams.Close(abort) != nil {
		return failure("output_protection_failed")
	}
	return nil
}

func (b *BoundInput) Close() {
	if !b.finished && b.Streams != nil {
		b.FinishOutput(true)
	}
	// No spawn callback means execution never crossed the OS start boundary.
	b.Complete("canceled")
	if b.Payload != nil {
		b.Payload.Destroy()
		b.Payload = nil
	}
}

// finishInputPhase reads only public operation metadata, never configuration or
// values. A failed candidate before admission must not imply the old app stopped.
func (s Store) finishInputPhase(record *Record) {
	if record.InputSnapshotID == "" || record.StartedAt != nil {
		return
	}
	if saved, err := s.read(record.ID); err == nil && saved.InputSnapshotID == record.InputSnapshotID {
		record.LaunchPhase = saved.LaunchPhase
	}
	if record.Previous == "" {
		return
	}
	if record.LaunchPhase == "committed" {
		record.Outage = true
		return
	}
	if previous, err := s.read(record.Previous); err == nil && previous.EndedAt != nil {
		record.Outage = true
	}
}
