package execution

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

const DefaultPreparationTimeout = 30 * time.Second

type InputMetadata struct {
	Delivery        *values.InputVersion    `json:"delivery,omitempty"`
	Output          redact.Plan             `json:"output"`
	InputID         string                  `json:"input_id"`
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
	consumed *atomic.Bool
	body     *vault.Material
	metadata InputMetadata
}
type inputWire struct {
	VaultDirectory string              `json:"vault_directory,omitempty"`
	Served         []ports.Assignment  `json:"served,omitempty"`
	Format         int                 `json:"format"`
	Metadata       InputMetadata       `json:"metadata"`
	Args           []string            `json:"args"`
	Environment    []string            `json:"environment"`
	Directory      string              `json:"directory"`
	Executable     string              `json:"executable"`
	Ready          *project.ReadyProbe `json:"ready,omitempty"`
	Patterns       [][]byte            `json:"patterns"`
}

// ProcessInput is an owned, explicit spawn/redactor boundary. Destroy it after
// use; decoded string copies and OS process environments are not a secure heap.
type ProcessInput struct {
	VaultDirectory string
	Served         []ports.Assignment
	Args           []string
	Environment    []string
	Directory      string
	Executable     string
	Ready          *project.ReadyProbe
	Patterns       [][]byte
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
	metadata.Delivery = metadata.Delivery.Clone()
	if metadata.Values.Public != nil {
		copy := *metadata.Values.Public
		metadata.Values.Public = &copy
	}
	if metadata.Values.Launch != nil {
		ticket := *metadata.Values.Launch
		metadata.Values.Launch = &ticket
	}
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
	return &InputPayload{consumed: &atomic.Bool{}, body: vault.NewMaterial(b), metadata: cloneInputMetadata(wire.Metadata)}, nil
}
func validateInputWire(wire inputWire) *protocol.Error {
	if !wire.Metadata.Output.Valid() {
		return snapshotError("invalid_input_snapshot")
	}
	id, err := hex.DecodeString(wire.Metadata.InputID)
	if err != nil || len(id) != 16 || len(wire.Metadata.InputID) != 32 {
		return snapshotError("invalid_input_snapshot")
	}
	if ticket := wire.Metadata.Values.Public; ticket != nil {
		identity := wire.Metadata.Values.Identity
		if wire.Metadata.Values.Launch != nil || !filepath.IsAbs(wire.VaultDirectory) || !ticket.Valid() || ticket.Profile != wire.Metadata.Profile || ticket.ProfileID != identity.ProfileID || ticket.StorageGeneration != identity.StorageGeneration || wire.Metadata.Values.KeyEpoch != 0 || wire.Metadata.Values.IntegrityVerified {
			return snapshotError("invalid_input_snapshot")
		}
	}
	if ticket := wire.Metadata.Values.Launch; ticket != nil {
		identity := wire.Metadata.Values.Identity
		if !filepath.IsAbs(wire.VaultDirectory) || ticket.Profile != wire.Metadata.Profile || ticket.ProfileID == "" || ticket.ProfileID != identity.ProfileID || ticket.StorageGeneration == "" || ticket.StorageGeneration != identity.StorageGeneration || ticket.KeyEpoch != wire.Metadata.Values.KeyEpoch {
			return snapshotError("invalid_input_snapshot")
		}
	}
	if wire.Format != 1 || !filepath.IsAbs(wire.Directory) || !filepath.IsAbs(wire.Executable) || wire.Environment == nil || wire.Metadata.PreparedAt.IsZero() || !wire.Metadata.PrepareDeadline.After(wire.Metadata.PreparedAt) {
		return snapshotError("invalid_input_snapshot")
	}
	if wire.Metadata.PrepareDeadline.After(wire.Metadata.PreparedAt.Add(DefaultPreparationTimeout)) {
		return snapshotError("invalid_input_snapshot")
	}
	if !vault.ValidSelection(wire.Metadata.Selection) || wire.Metadata.Selection == "" {
		return snapshotError("invalid_input_snapshot")
	}
	if delivered := wire.Metadata.Delivery; delivered != nil {
		expected, err := values.NewInputVersion(wire.Metadata.Values, wire.Metadata.Env, delivered.Spec)
		if err != nil || !reflect.DeepEqual(delivered, expected) || delivered.Spec.Selection != wire.Metadata.Selection || delivered.Spec.Inject != wire.Metadata.Inject {
			return snapshotError("invalid_input_snapshot")
		}
	}
	if wire.Metadata.Values.Identity.Revision != "" {
		selected := wire.Metadata.Values.Selection
		if selected == "" {
			selected = "all"
		}
		if selected != wire.Metadata.Selection {
			return snapshotError("invalid_input_snapshot")
		}
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
	return &ProcessInput{VaultDirectory: wire.VaultDirectory, Served: wire.Served, Args: wire.Args, Environment: wire.Environment, Directory: wire.Directory, Executable: wire.Executable, Ready: wire.Ready, Patterns: wire.Patterns}, nil
}

// EncodeForLauncher returns an owned sensitive copy, never a public receipt or
// on-disk plaintext file. Callers bind the chosen launcher before admission.
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
	p.Served = nil
	clear(p.Args)
	clear(p.Environment)
	p.Args = nil
	p.Environment = nil
	p.VaultDirectory = ""
	p.Directory = ""
	p.Executable = ""
	p.Ready = nil
}
