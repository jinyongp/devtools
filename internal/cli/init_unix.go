//go:build !windows

package cli

import (
	"context"
	"io"
	"os"

	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/unix"
)

// Poll the caller's terminal directly: cancellation leaves no blocked reader.
// Unlike secret entry, profile input keeps canonical editing and visible echo.
func readProfileLine(ctx context.Context, streams IO, prompt string) (string, *protocol.Error) {
	defer progress.Suspend(ctx)()
	failure := func() *protocol.Error {
		return initInputError("input_required", "Profile input is unavailable; provide --profile explicitly.", 3)
	}
	file, ok := streams.In.(*os.File)
	if !ok {
		return "", failure()
	}
	fd := int(file.Fd())
	state, err := unix.IoctlGetTermios(fd, terminalReadState)
	if err != nil {
		return "", failure()
	}
	visible := *state
	visible.Lflag |= unix.ECHO | unix.ICANON | unix.ISIG
	visible.Iflag |= unix.ICRNL
	if unix.IoctlSetTermios(fd, terminalWriteState, &visible) != nil {
		return "", failure()
	}
	defer func() {
		setting := uint(terminalWriteState)
		if ctx.Err() != nil {
			setting = terminalFlushState
		}
		_ = unix.IoctlSetTermios(fd, setting, state)
	}()
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return "", failure()
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags|unix.O_NONBLOCK); err != nil {
		return "", failure()
	}
	defer func() { _, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags) }()
	if _, err := io.WriteString(streams.Err, prompt); err != nil {
		return "", initInputError("io_error", "Cannot write profile prompt.", 1)
	}
	body := make([]byte, 0, 129)
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	var buffer [1]byte
	for {
		if failure := protocol.ContextError(ctx.Err()); failure != nil {
			return "", failure
		}
		_, err := unix.Poll(poll, 50)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return "", failure()
		}
		if poll[0].Revents == 0 {
			continue
		}
		n, err := unix.Read(fd, buffer[:])
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil || n == 0 {
			return "", failure()
		}
		if buffer[0] == '\n' {
			return string(body), nil
		}
		if len(body) < 129 {
			body = append(body, buffer[0])
		}
	}
}
