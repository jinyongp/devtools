package cli

import (
	"context"
	"io"

	"github.com/jinyongp/devtools/internal/protocol"
)

// readBoundedInput owns its returned buffer. On cancellation it closes closable
// input and waits for the read worker to release its buffer. An embedding host
// supplying a blocking non-Closer remains responsible for ending that read.
func readBoundedInput(ctx context.Context, reader io.Reader, limit int64, role string) ([]byte, *protocol.Error) {
	if ctx.Err() != nil {
		return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
	}
	prepared, cleanup, err := cancellableInput(reader)
	if err != nil {
		return nil, protocol.InputError(role, "prepare_input", err)
	}
	defer cleanup()
	reader = prepared
	type result struct {
		body []byte
		err  error
	}
	ch := make(chan result)
	done := make(chan struct{})
	go func() {
		defer close(done)
		body, err := io.ReadAll(io.LimitReader(reader, limit+1))
		select {
		case ch <- result{body, err}:
		case <-ctx.Done():
			clear(body)
		}
	}()
	select {
	case <-ctx.Done():
		if closer, ok := reader.(io.Closer); ok {
			_ = closer.Close()
			<-done
		}
		return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
	case read := <-ch:
		if ctx.Err() != nil {
			clear(read.body)
			return nil, protocol.NewError("canceled", "Execution canceled.", 130, nil)
		}
		if read.err != nil {
			clear(read.body)
			return nil, protocol.InputError(role, "read_input", read.err)
		}
		return read.body, nil
	}
}

func inputLimitError(role string, limit, observed int64, exact, managed bool) *protocol.Error {
	code, exit := "vault_input_too_large", 3
	if managed {
		code, exit = "invalid_argument", 2
	}
	return protocol.NewError(code, "The selected input exceeds its byte limit; reduce the input before retrying.", exit, map[string]any{
		"input_role": role, "resource": "input", "stage": "read_input", "phase": "prepare",
		"facts": map[string]any{"input_role": role, "observed": observed, "limit": limit, "observed_exact": exact},
	})
}
