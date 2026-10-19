package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

const DefaultPreparationTimeout = 30 * time.Second

type InputMetadata struct {
	Inject          bool                    `json:"inject"`
	Selection       string                  `json:"selection"`
	Parent          process.ParentPolicy    `json:"parent"`
	Profile         string                  `json:"profile"`
	Command         string                  `json:"command,omitempty"`
	Env             string                  `json:"env,omitempty"`
	PreparedAt      time.Time               `json:"prepared_at"`
	PrepareDeadline time.Time               `json:"prepare_deadline"`
	Values          values.SnapshotMetadata `json:"values"`
}

// InputPayload contains the actual immutable process input. Its encoded bytes
// cross only the explicit launcher boundary; normal JSON/fmt cannot reveal it.
type InputPayload struct {
	body     *vault.Material
	metadata InputMetadata
}
type inputWire struct {
	Format      int                 `json:"format"`
	Metadata    InputMetadata       `json:"metadata"`
	Args        []string            `json:"args"`
	Environment []string            `json:"environment"`
	Directory   string              `json:"directory"`
	Executable  string              `json:"executable"`
	Ready       *project.ReadyProbe `json:"ready,omitempty"`
	Patterns    [][]byte            `json:"patterns"`
}

// ProcessInput is an owned, explicit spawn/redactor boundary. Destroy it after
// use; decoded string copies and OS process environments are not a secure heap.
type ProcessInput struct {
	Args        []string
	Environment []string
	Directory   string
	Executable  string
	Ready       *project.ReadyProbe
	Patterns    [][]byte
}

func (InputPayload) MarshalJSON() ([]byte, error) {
	return nil, errors.New("execution payload cannot be serialized")
}
func (InputPayload) MarshalText() ([]byte, error) {
	return nil, errors.New("execution payload cannot be serialized")
}
func (InputPayload) Format(s fmt.State, _ rune) { io.WriteString(s, "[REDACTED]") }
func (ProcessInput) MarshalJSON() ([]byte, error) {
	return nil, errors.New("process input cannot be serialized")
}
func (ProcessInput) MarshalText() ([]byte, error) {
	return nil, errors.New("process input cannot be serialized")
}
func (ProcessInput) Format(s fmt.State, _ rune) { io.WriteString(s, "[REDACTED]") }
func (Prepared) MarshalJSON() ([]byte, error) {
	return nil, errors.New("prepared execution cannot be serialized")
}
func (Prepared) Format(s fmt.State, _ rune) { io.WriteString(s, "[REDACTED]") }

func snapshotError(code string) *protocol.Error {
	return protocol.NewError(code, "Prepared execution input is unavailable or expired; prepare a new execution.", 3, nil)
}
func cloneInputMetadata(metadata InputMetadata) InputMetadata {
	metadata.Parent.Allow = append([]string{}, metadata.Parent.Allow...)
	metadata.Values.Slots = append([]vault.SlotReference{}, metadata.Values.Slots...)
	if metadata.Values.EarliestExpiry != nil {
		expiry := *metadata.Values.EarliestExpiry
		metadata.Values.EarliestExpiry = &expiry
	}
	return metadata
}
func (p *InputPayload) Metadata() InputMetadata {
	if p == nil {
		return InputMetadata{}
	}
	return cloneInputMetadata(p.metadata)
}
func (p *InputPayload) Validate(now time.Time) *protocol.Error {
	if p == nil || p.body == nil {
		return snapshotError("input_snapshot_unavailable")
	}
	if !now.Before(p.metadata.PrepareDeadline) {
		return snapshotError("input_snapshot_expired")
	}
	if expiry := p.metadata.Values.EarliestExpiry; expiry != nil && !now.Before(*expiry) {
		return snapshotError("value_expired")
	}
	return nil
}
func newInputPayload(wire inputWire) (*InputPayload, *protocol.Error) {
	if e := validateInputWire(wire); e != nil {
		return nil, e
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, snapshotError("invalid_input_snapshot")
	}
	defer clear(b)
	if len(b) > vault.MaxFrameBytes {
		return nil, protocol.NewError("execution_input_too_large", "Execution payload exceeds the supported transport limit.", 2, nil)
	}
	return &InputPayload{body: vault.NewMaterial(b), metadata: cloneInputMetadata(wire.Metadata)}, nil
}
func validateInputWire(wire inputWire) *protocol.Error {
	if wire.Format != 1 || !filepath.IsAbs(wire.Directory) || !filepath.IsAbs(wire.Executable) || wire.Environment == nil || wire.Metadata.PreparedAt.IsZero() || !wire.Metadata.PrepareDeadline.After(wire.Metadata.PreparedAt) {
		return snapshotError("invalid_input_snapshot")
	}
	if wire.Metadata.PrepareDeadline.After(wire.Metadata.PreparedAt.Add(DefaultPreparationTimeout)) {
		return snapshotError("invalid_input_snapshot")
	}
	if wire.Metadata.Selection != "all" {
		return snapshotError("invalid_input_snapshot")
	}
	if e := process.ValidateExecInput(wire.Args, wire.Environment); e != nil {
		return e
	}
	if wire.Ready != nil {
		if !wire.Ready.Valid() {
			return snapshotError("invalid_input_snapshot")
		}
		if e := process.ValidateExecInput(wire.Ready.Exec, wire.Environment); e != nil {
			return e
		}
	}
	return nil
}
func (p *InputPayload) Open() (*ProcessInput, *protocol.Error) {
	if p == nil || p.body == nil {
		return nil, snapshotError("input_snapshot_unavailable")
	}
	b := p.body.CopyBytes()
	defer clear(b)
	var wire inputWire
	if len(b) > vault.MaxFrameBytes || vault.StrictDecode(b, &wire) != nil {
		for _, pattern := range wire.Patterns {
			clear(pattern)
		}
		return nil, snapshotError("invalid_input_snapshot")
	}
	if e := validateInputWire(wire); e != nil {
		for _, pattern := range wire.Patterns {
			clear(pattern)
		}
		return nil, e
	}
	return &ProcessInput{Args: wire.Args, Environment: wire.Environment, Directory: wire.Directory, Executable: wire.Executable, Ready: wire.Ready, Patterns: wire.Patterns}, nil
}

// EncodeForLauncher returns an owned sensitive copy, never a public receipt or
// on-disk plaintext file. A08 supplies authenticated handoff and admission.
func (p *InputPayload) EncodeForLauncher() (*vault.Material, *protocol.Error) {
	if p == nil || p.body == nil {
		return nil, snapshotError("input_snapshot_unavailable")
	}
	b := p.body.CopyBytes()
	defer clear(b)
	return vault.NewMaterial(b), nil
}
func DecodeLauncherInput(encoded *vault.Material) (*InputPayload, *protocol.Error) {
	b := encoded.CopyBytes()
	defer clear(b)
	var wire inputWire
	if len(b) == 0 || len(b) > vault.MaxFrameBytes || vault.StrictDecode(b, &wire) != nil {
		for _, pattern := range wire.Patterns {
			clear(pattern)
		}
		return nil, snapshotError("invalid_input_snapshot")
	}
	defer func() {
		for _, pattern := range wire.Patterns {
			clear(pattern)
		}
	}()
	return newInputPayload(wire)
}
func (p *InputPayload) Destroy() {
	if p != nil {
		p.body.Destroy()
		p.body = nil
	}
}
func (p *ProcessInput) Destroy() {
	if p == nil {
		return
	}
	for _, pattern := range p.Patterns {
		clear(pattern)
	}
	p.Patterns = nil
	clear(p.Args)
	clear(p.Environment)
	p.Args = nil
	p.Environment = nil
	p.Directory = ""
	p.Executable = ""
	p.Ready = nil
}
