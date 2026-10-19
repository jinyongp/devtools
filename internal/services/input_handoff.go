package services

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/protocol"
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
func (c *launcherInputChannel) expect(value byte) error {
	var b [1]byte
	if _, err := io.ReadFull(c.read, b[:]); err != nil {
		return err
	}
	if b[0] != value {
		return errors.New("launcher handoff rejected")
	}
	return nil
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
	if c.expect(launchBound) != nil {
		return "", failure("launcher_unavailable")
	}
	return snapshotID, nil
}
func (c *launcherInputChannel) admit() *protocol.Error {
	if _, err := c.write.Write([]byte{launchAdmitted}); err != nil {
		return failure("launcher_unavailable")
	}
	if c.expect(launchAdmitted) != nil {
		return failure("launch_not_admitted")
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
	Payload   *execution.InputPayload
	admission *vault.LaunchAdmission
}

func ReceiveLauncherInput(ctx context.Context, in io.ReadCloser, out io.WriteCloser, record Record, data string) (*BoundInput, *protocol.Error) {
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
	matches := metadata.InputID == record.InputSnapshotID && metadata.Profile == record.Profile && metadata.Command == record.Command && metadata.Env == record.Env && input.Directory == record.Directory
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
	if ticket := metadata.Values.Launch; ticket != nil {
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
func (b *BoundInput) Close() {
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
