package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const transferPassphraseLimit = 64 << 10

func passphraseRequiredError(command string) *protocol.Error {
	return protocol.NewError("passphrase_required", "A profile transfer passphrase is required.", 3, map[string]any{
		"remedies": []protocol.Remedy{
			{
				Argv:           []string{"devtools", "profile", command, "--passphrase-file", "PATH"},
				RequiredInputs: []string{"passphrase-file"},
				Message:        "Provide the passphrase through a regular file for non-interactive use.",
			},
			{
				Argv:           []string{"devtools", "profile", command, "--passphrase-stdin"},
				RequiredInputs: []string{"stdin"},
				Message:        "Pipe the passphrase to stdin for non-interactive use.",
			},
		},
	})
}

func normalizeTransferPassphrase(data []byte) (string, *protocol.Error) {
	if len(data) > transferPassphraseLimit {
		return "", argumentError("Passphrase input is too large.", "")
	}
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
		if len(data) > 0 && data[len(data)-1] == '\r' {
			data = data[:len(data)-1]
		}
	}
	if len(data) == 0 {
		return "", protocol.NewError("passphrase_required", "Profile transfer passphrase cannot be empty.", 3, nil)
	}
	if !utf8.Valid(data) || strings.ContainsAny(string(data), "\r\n") {
		return "", argumentError("Passphrase input must be one UTF-8 line.", "")
	}
	return string(data), nil
}

func readPassphraseReader(ctx context.Context, reader io.Reader) (string, *protocol.Error) {
	type result struct {
		body []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		body, err := io.ReadAll(io.LimitReader(reader, transferPassphraseLimit+1))
		ch <- result{body: body, err: err}
	}()
	select {
	case <-ctx.Done():
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
		}
		return "", protocol.NewError("canceled", "Execution canceled.", 130, nil)
	case read := <-ch:
		if read.err != nil {
			return "", protocol.NewError("io_error", "Cannot read passphrase input.", 1, nil)
		}
		passphrase, inputErr := normalizeTransferPassphrase(read.body)
		for i := range read.body {
			read.body[i] = 0
		}
		return passphrase, inputErr
	}
}

func readPassphraseSource(ctx context.Context, streams IO, filePath string, fromStdin bool, command string) (string, *protocol.Error) {
	if (filePath != "") == fromStdin {
		return "", argumentError("Select exactly one passphrase input: --passphrase-file or --passphrase-stdin.", "")
	}
	if filePath != "" {
		file, err := os.OpenFile(filePath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return "", protocol.NewError("io_error", "Cannot read passphrase input.", 1, nil)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", argumentError("Passphrase file input requires a regular file.", "passphrase-file")
		}
		return readPassphraseReader(ctx, file)
	}
	if streams.In == nil {
		return "", passphraseRequiredError(command)
	}
	if file, ok := streams.In.(*os.File); ok {
		info, err := file.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return "", argumentError("Passphrase stdin requires a pipe or file redirect.", "passphrase-stdin")
		}
	}
	return readPassphraseReader(ctx, streams.In)
}

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

func acquireTransferPassphrase(ctx context.Context, streams IO, options map[string]string, command string, confirm bool, prompt func(context.Context, IO, string) (string, *protocol.Error)) (string, *protocol.Error) {
	filePath := options["passphrase-file"]
	fromStdin := options["passphrase-stdin"] == "true"
	if filePath != "" || fromStdin {
		if filePath != "" && fromStdin {
			return "", argumentError("Select exactly one passphrase input: --passphrase-file or --passphrase-stdin.", "")
		}
		return readPassphraseSource(ctx, streams, filePath, fromStdin, command)
	}
	if prompt == nil {
		return "", passphraseRequiredError(command)
	}
	first, err := prompt(ctx, streams, "Profile transfer passphrase: ")
	if err != nil {
		if err.Code == "passphrase_required" {
			return "", passphraseRequiredError(command)
		}
		return "", err
	}
	if first == "" {
		return "", passphraseRequiredError(command)
	}
	if !confirm {
		return first, nil
	}
	second, confirmErr := prompt(ctx, streams, "Confirm profile transfer passphrase: ")
	if confirmErr != nil {
		return "", confirmErr
	}
	if first != second {
		return "", protocol.NewError("passphrase_mismatch", "Profile transfer passphrases do not match.", 3, nil)
	}
	return first, nil
}

func passphraseOptionConflict(options map[string]string, incompatible ...string) *protocol.Error {
	hasPassphrase := options["passphrase-file"] != "" || options["passphrase-stdin"] == "true"
	if !hasPassphrase {
		return nil
	}
	for _, name := range incompatible {
		if options[name] != "" && options[name] != "false" {
			return argumentError("Passphrase input cannot be combined with "+name+".", name)
		}
	}
	return nil
}
