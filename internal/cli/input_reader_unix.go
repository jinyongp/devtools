//go:build !windows

package cli

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// Inherited stdin may be a blocking descriptor outside Go's poller. Duplicate
// it after enabling nonblocking mode so Close can interrupt the owned read.
// Restore the original flags after the worker ends; dup shares those flags.
func cancellableInput(reader io.Reader) (io.Reader, func(), error) {
	f, ok := reader.(*os.File)
	if !ok {
		return reader, func() {}, nil
	}
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		return reader, func() {}, nil
	}
	raw, err := f.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var setupErr error
	fd, flags := -1, 0
	err = raw.Control(func(original uintptr) {
		flags, setupErr = unix.FcntlInt(original, unix.F_GETFL, 0)
		if setupErr != nil {
			return
		}
		fd, setupErr = unix.Dup(int(original))
		if setupErr != nil {
			return
		}
		unix.CloseOnExec(fd)
		setupErr = unix.SetNonblock(fd, true)
	})
	if err != nil || setupErr != nil {
		if fd >= 0 {
			unix.Close(fd)
		}
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, setupErr
	}
	owned := os.NewFile(uintptr(fd), "devtools-input")
	return owned, func() {
		owned.Close()
		_ = raw.Control(func(original uintptr) { _ = unix.SetNonblock(int(original), flags&unix.O_NONBLOCK != 0) })
	}, nil
}
