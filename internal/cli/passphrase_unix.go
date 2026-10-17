//go:build !windows

package cli

import (
	"context"
	"io"
	"os"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func readTerminalPassphrase(ctx context.Context, streams IO, prompt string) (string, *protocol.Error) {
	if err := ctx.Err(); err != nil {
		return "", protocol.NewError("canceled", "Execution canceled.", 130, nil)
	}
	file, ok := streams.In.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return "", passphraseRequiredError("import")
	}
	fd := int(file.Fd())
	state, err := unix.IoctlGetTermios(fd, terminalReadState)
	if err != nil {
		return "", protocol.NewError("io_error", "Cannot read terminal settings.", 1, nil)
	}
	quiet := *state
	quiet.Lflag &^= unix.ECHO | unix.ECHONL
	quiet.Lflag |= unix.ICANON | unix.ISIG
	quiet.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, terminalWriteState, &quiet); err != nil {
		return "", protocol.NewError("io_error", "Cannot disable terminal echo.", 1, nil)
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
		return "", protocol.NewError("io_error", "Cannot read terminal input flags.", 1, nil)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags|unix.O_NONBLOCK); err != nil {
		return "", protocol.NewError("io_error", "Cannot configure terminal input.", 1, nil)
	}
	defer func() { _, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags) }()
	// A visible prompt must mean input can already be sent without being echoed.
	if streams.Err != nil {
		if _, err := io.WriteString(streams.Err, prompt); err != nil {
			return "", protocol.NewError("io_error", "Cannot write passphrase prompt.", 1, nil)
		}
	}
	body, err := readTerminalPassphraseLine(ctx, fd)
	if streams.Err != nil {
		_, _ = io.WriteString(streams.Err, "\n")
	}
	if err != nil {
		if ctx.Err() != nil {
			return "", protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		return "", protocol.NewError("io_error", "Cannot read passphrase from terminal.", 1, nil)
	}
	passphrase, inputErr := normalizeTransferPassphrase(body)
	for i := range body {
		body[i] = 0
	}
	return passphrase, inputErr
}

// Poll canonical terminal input so cancellation does not leave a blocked
// password reader owning the terminal or restoring stale settings later.
func readTerminalPassphraseLine(ctx context.Context, fd int) ([]byte, error) {
	body := make([]byte, 0)
	defer func() {
		if ctx.Err() != nil {
			for i := range body {
				body[i] = 0
			}
		}
	}()
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	var buffer [1]byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, err := unix.Poll(poll, 50)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		if poll[0].Revents == 0 {
			continue
		}
		n, err := unix.Read(fd, buffer[:])
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			if len(body) > 0 {
				return body, nil
			}
			return nil, io.EOF
		}
		switch buffer[0] {
		case '\n':
			return body, nil
		case '\r':
		case '\b':
			if len(body) > 0 {
				body = body[:len(body)-1]
			}
		default:
			body = append(body, buffer[0])
			if len(body) > transferPassphraseLimit {
				return body, nil
			}
		}
	}
}
