package vault

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"time"
)

const maxSessionFrame = 128 << 10
const maxSessionConnections = 8

type sessionConnection interface {
	io.ReadWriteCloser
	SetDeadline(time.Time) error
}
type sessionListener interface {
	Accept(context.Context) (sessionConnection, error)
	Close() error
}

func writeSessionFrame(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return ErrInvalid
	}
	defer clear(b)
	if len(b) == 0 || len(b) > maxSessionFrame {
		return ErrTooLarge
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(b)))
	if err = writeAll(w, length[:]); err != nil {
		return err
	}
	return writeAll(w, b)
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func readSessionFrame(r io.Reader, value any) error {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(length[:])
	if n == 0 || n > maxSessionFrame {
		return ErrTooLarge
	}
	b := make([]byte, n)
	defer clear(b)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return StrictDecode(b, value)
}
