package valuehandoff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/vault"
)

type privateRedaction struct {
	Protocol int      `json:"protocol"`
	Patterns [][]byte `json:"patterns"`
}

func (v *storedValue) UnmarshalJSON(body []byte) error {
	var input struct {
		Key   string          `json:"key"`
		Kind  vault.ValueKind `json:"kind"`
		Value *string         `json:"value"`
	}
	if vault.StrictDecode(body, &input) != nil || input.Value == nil {
		return vault.ErrInvalid
	}
	*v = storedValue{input.Key, input.Kind, *input.Value}
	return nil
}

type privatePayload struct {
	Protocol            int              `json:"protocol"`
	Type                string           `json:"type"`
	ExecutionID         string           `json:"execution_id"`
	InputID             string           `json:"input_id"`
	DeliveryID          string           `json:"delivery_id"`
	ExternalExecutionID string           `json:"external_execution_id"`
	Values              []storedValue    `json:"values"`
	Redaction           privateRedaction `json:"redaction"`
}

func (privatePayload) MarshalJSON() ([]byte, error) { return nil, vault.ErrDenied }
func (privatePayload) MarshalText() ([]byte, error) { return nil, vault.ErrDenied }
func (privatePayload) Format(s fmt.State, _ rune)   { io.WriteString(s, "[REDACTED]") }
func (p *privatePayload) destroy() {
	for _, pattern := range p.Redaction.Patterns {
		clear(pattern)
	}
	clear(p.Redaction.Patterns)
	clear(p.Values)
}
func patternsFor(values []storedValue) [][]byte {
	patterns := [][]byte{}
	for _, v := range values {
		if v.Kind == vault.Secret && v.Value != "" {
			patterns = append(patterns, []byte(v.Value))
		}
	}
	return patterns
}
func (p privatePayload) validate() error {
	if p.Protocol != 1 || p.Type != "payload" || !idRE.MatchString(p.ExecutionID) || !idRE.MatchString(p.InputID) || !idRE.MatchString(p.DeliveryID) || !opaque(p.ExternalExecutionID) || p.Values == nil || len(p.Values) > 5000 || p.Redaction.Protocol != 1 || p.Redaction.Patterns == nil {
		return vault.ErrInvalid
	}
	seen := map[string]bool{}
	for _, v := range p.Values {
		if !keyRE.MatchString(v.Key) || seen[v.Key] || (v.Kind != vault.Secret && v.Kind != vault.Public) || !utf8.ValidString(v.Value) || strings.ContainsRune(v.Value, 0) {
			return vault.ErrInvalid
		}
		seen[v.Key] = true
	}
	expected := patternsFor(p.Values)
	defer func() {
		for _, p := range expected {
			clear(p)
		}
	}()
	if len(expected) != len(p.Redaction.Patterns) {
		return vault.ErrInvalid
	}
	for i := range expected {
		if !bytes.Equal(expected[i], p.Redaction.Patterns[i]) {
			return vault.ErrInvalid
		}
	}
	matcher, err := redact.New(p.Redaction.Patterns)
	if err != nil {
		return vault.ErrTooLarge
	}
	matcher.Destroy()
	return nil
}
func encodePayload(p privatePayload) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	type encoded privatePayload
	b, err := json.Marshal(encoded(p))
	if err != nil {
		return nil, err
	}
	if len(b) > maxPayloadFrame {
		clear(b)
		return nil, vault.ErrTooLarge
	}
	return b, nil
}
func decodePayload(b []byte) (privatePayload, error) {
	type encoded privatePayload
	var p encoded
	if err := vault.StrictDecode(b, &p); err != nil {
		partial := privatePayload(p)
		partial.destroy()
		return privatePayload{}, err
	}
	value := privatePayload(p)
	if err := value.validate(); err != nil {
		value.destroy()
		return privatePayload{}, err
	}
	return value, nil
}

// ReceivedInput crosses only the caller's explicit process boundary. Ordinary
// JSON/fmt refuses values and patterns. Keep it alive until all output reaches EOF.
type ReceivedInput struct{ payload privatePayload }

func (ReceivedInput) MarshalJSON() ([]byte, error) { return nil, vault.ErrDenied }
func (ReceivedInput) MarshalText() ([]byte, error) { return nil, vault.ErrDenied }
func (ReceivedInput) Format(s fmt.State, _ rune)   { io.WriteString(s, "[REDACTED]") }
func (p *ReceivedInput) Identity() (execution, input, job, delivery string) {
	return p.payload.ExecutionID, p.payload.InputID, p.payload.ExternalExecutionID, p.payload.DeliveryID
}
func (p *ReceivedInput) Environment() []string {
	out := make([]string, 0, len(p.payload.Values))
	for _, v := range p.payload.Values {
		out = append(out, v.Key+"="+v.Value)
	}
	return out
}
