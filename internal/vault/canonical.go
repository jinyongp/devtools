package vault

import (
	"encoding/binary"
	"unicode/utf8"
)

// Encoder produces the fixed-schema authentication representation documented in
// docs/var-sec-contract.md. Callers own field order and sorted collection order;
// maps and JSON serialization must not be used as the authentication input.
type Encoder struct {
	data []byte
	err  error
}

func NewEncoder(domain string) *Encoder {
	e := &Encoder{data: []byte("DTV1")}
	if domain == "" {
		e.err = ErrInvalid
	}
	e.String(domain)
	return e
}

func (e *Encoder) append(data []byte) {
	if e.err != nil {
		return
	}
	if len(data) > MaxDocumentBytes-len(e.data) {
		e.err = ErrTooLarge
		return
	}
	e.data = append(e.data, data...)
}

func (e *Encoder) Uint(value uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], value)
	e.append(b[:])
}

func (e *Encoder) Bool(value bool) {
	b := byte(0)
	if value {
		b = 1
	}
	e.append([]byte{b})
}

func (e *Encoder) Blob(value []byte) {
	if e.err != nil {
		return
	}
	if len(value) > MaxDocumentBytes || len(value)+4 > MaxDocumentBytes-len(e.data) {
		e.err = ErrTooLarge
		return
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	e.append(size[:])
	e.append(value)
}

func (e *Encoder) String(value string) {
	if e.err != nil {
		return
	}
	if len(value) > MaxDocumentBytes {
		e.err = ErrTooLarge
		return
	}
	if !utf8.ValidString(value) {
		e.err = ErrInvalid
		return
	}
	e.Blob([]byte(value))
}

// Finish returns the owned representation, or no bytes on any encoding error.
// An encoder is single-use and must not be shared with another goroutine.
func (e *Encoder) Finish() ([]byte, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.data, nil
}
