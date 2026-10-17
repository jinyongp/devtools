package cli

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var peekTaskPipe = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

func taskPipeAvailable(file *os.File) (uint32, error) {
	var available uint32
	ok, _, err := peekTaskPipe.Call(file.Fd(), 0, 0, 0, uintptr(unsafe.Pointer(&available)), 0)
	if ok == 0 {
		if err == syscall.Errno(0) {
			err = windows.ERROR_INVALID_HANDLE
		}
		return 0, err
	}
	return available, nil
}

// Anonymous Windows pipes can be synchronous and reject SetReadDeadline. Poll
// availability and read only those bytes with this exclusive caller-owned input;
// cancellation never closes the caller's handle or leaves a consuming goroutine.
func readTaskPipe(ctx context.Context, file *os.File, limit int) ([]byte, error, bool) {
	body := make([]byte, 0, 4096)
	for len(body) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err, true
		}
		available, err := taskPipeAvailable(file)
		if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA) {
			return body, nil, true
		}
		if err != nil {
			return nil, err, true
		}
		if available == 0 {
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err(), true
			case <-timer.C:
			}
			continue
		}
		count := min(int(available), limit-len(body), 65536)
		chunk := make([]byte, count)
		if err = ctx.Err(); err != nil {
			return nil, err, true
		}
		n, readErr := file.Read(chunk)
		body = append(body, chunk[:n]...)
		if errors.Is(readErr, io.EOF) {
			return body, nil, true
		}
		if readErr != nil {
			return nil, readErr, true
		}
	}
	return body, nil, true
}
