//go:build linux

package netsetup

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/unix"
)

func expectedCapability() []byte {
	b := make([]byte, 20)
	binary.LittleEndian.PutUint32(b, 0x02000001)
	binary.LittleEndian.PutUint32(b[4:], 1<<10)
	return b
}
func capability(f *os.File) (bool, *protocol.Error) {
	b := make([]byte, 64)
	n, e := unix.Fgetxattr(int(f.Fd()), "security.capability", b)
	if errors.Is(e, unix.ENODATA) {
		return false, nil
	}
	if errors.Is(e, unix.EOPNOTSUPP) {
		return false, failure("capability_unsupported")
	}
	if e != nil {
		return false, failure("capability_unavailable")
	}
	return bytes.Equal(b[:n], expectedCapability()), nil
}
func setCapability(f *os.File, enable bool) *protocol.Error {
	b := make([]byte, 64)
	n, e := unix.Fgetxattr(int(f.Fd()), "security.capability", b)
	if e != nil && !errors.Is(e, unix.ENODATA) {
		if errors.Is(e, unix.EOPNOTSUPP) {
			return failure("capability_unsupported")
		}
		return failure("capability_unavailable")
	}
	if e == nil && !bytes.Equal(b[:n], expectedCapability()) {
		return failure("capability_conflict")
	}
	if enable {
		e = unix.Fsetxattr(int(f.Fd()), "security.capability", expectedCapability(), 0)
	} else if e == nil {
		e = unix.Fremovexattr(int(f.Fd()), "security.capability")
	}
	if e != nil && !errors.Is(e, unix.ENODATA) {
		if errors.Is(e, unix.EOPNOTSUPP) {
			return failure("capability_unsupported")
		}
		return failure("permission_required")
	}
	return nil
}
