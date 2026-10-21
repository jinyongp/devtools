package valuehandoff

import (
	"encoding/json"
	"github.com/jinyongp/devtools/internal/vault"
)

// DecodeControl supplements the closed struct decoder: encoding/json accepts
// null for scalar Go fields, but the handoff protocol has no nullable inputs.
func DecodeControl(body []byte, out any) error {
	if len(body) > 128<<10 {
		return vault.ErrTooLarge
	}
	if err := vault.StrictDecode(body, out); err != nil {
		return err
	}
	var input any
	if err := json.Unmarshal(body, &input); err != nil {
		return vault.ErrInvalid
	}
	var nonNull func(any) bool
	nonNull = func(v any) bool {
		switch x := v.(type) {
		case nil:
			return false
		case []any:
			for _, child := range x {
				if !nonNull(child) {
					return false
				}
			}
		case map[string]any:
			for _, child := range x {
				if !nonNull(child) {
					return false
				}
			}
		}
		return true
	}
	if !nonNull(input) {
		return vault.ErrInvalid
	}
	return nil
}

func (s *Selection) UnmarshalJSON(body []byte) error {
	type plain Selection
	var decoded plain
	if err := vault.StrictDecode(body, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return vault.ErrInvalid
	}
	if _, present := fields["bundle"]; present && decoded.Bundle == "" {
		return vault.ErrInvalid
	}
	*s = Selection(decoded)
	return nil
}
