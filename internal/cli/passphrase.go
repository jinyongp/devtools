package cli

import (
	"context"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/protocol"
)

const transferPassphraseLimit = 64 << 10

func passphraseRequiredError(command string) *protocol.Error {
	return protocol.NewError("passphrase_required", "A profile transfer passphrase is required.", 3, map[string]any{
		"remedies": []protocol.Remedy{
			{
				Relation:       protocol.SameStoreAction,
				Effect:         "value_write",
				Argv:           []string{"devtools", "profile", command, "--passphrase-file", "PATH"},
				RequiredInputs: []string{"passphrase-file"},
				Message:        "Provide the passphrase through a regular file for non-interactive use.",
			},
			{
				Relation:       protocol.SameStoreAction,
				Effect:         "value_write",
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
	if ctx.Err() != nil {
		return "", protocol.NewError("canceled", "Execution canceled.", 130, nil)
	}
	type result struct {
		body []byte
		err  error
	}
	ch := make(chan result)
	go func() {
		body, err := io.ReadAll(io.LimitReader(reader, transferPassphraseLimit+1))
		select {
		case ch <- result{body: body, err: err}:
		case <-ctx.Done():
			clear(body)
		}
	}()
	select {
	case <-ctx.Done():
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
		}
		return "", protocol.NewError("canceled", "Execution canceled.", 130, nil)
	case read := <-ch:
		defer clear(read.body)
		if ctx.Err() != nil {
			clear(read.body)
			return "", protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		if read.err != nil {
			return "", protocol.InputError("passphrase", "read_input", read.err)
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
			return "", protocol.InputError("passphrase", "open_input", err)
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
