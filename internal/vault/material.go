package vault

import (
	"errors"
	"fmt"
	"io"
)

// Material is owned by one key or execution lifetime. Its contents must cross a
// deliberate byte-copy boundary; ordinary diagnostic serialization is forbidden.
// It is not a secure heap and cannot erase copies made by its consumers or the OS.
// Do not copy a Material value or use it concurrently with Destroy.
type Material struct {
	data []byte
}

func NewMaterial(data []byte) *Material {
	return &Material{data: append([]byte(nil), data...)}
}

// CopyBytes transfers an owned copy to a cryptographic or process boundary.
// The receiver is responsible for its lifetime and for clearing that copy.
func (m *Material) CopyBytes() []byte {
	if m == nil {
		return nil
	}
	return append([]byte(nil), m.data...)
}

func (m *Material) Destroy() {
	if m != nil {
		clear(m.data)
		m.data = nil
	}
}

func (Material) MarshalJSON() ([]byte, error) {
	return nil, errors.New("sensitive material cannot be serialized")
}

func (Material) MarshalText() ([]byte, error) {
	return nil, errors.New("sensitive material cannot be serialized")
}

func (Material) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[REDACTED]")
}
