package vault

import (
	"errors"
	"fmt"
	"io"
)

func (EffectiveValue) MarshalJSON() ([]byte, error) {
	return nil, errors.New("effective values cannot be serialized")
}
func (EffectiveValue) MarshalText() ([]byte, error) {
	return nil, errors.New("effective values cannot be serialized")
}
func (EffectiveValue) Format(state fmt.State, _ rune) { io.WriteString(state, "[REDACTED]") }
