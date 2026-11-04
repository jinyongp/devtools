package cli

import (
	"context"
	"io"
	"os"
	"time"
	"unicode/utf16"

	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

func readProfileLine(ctx context.Context, streams IO, prompt string) (value string, result *protocol.Error) {
	defer progress.Suspend(ctx)()
	failure := func() *protocol.Error {
		return initInputError("input_required", "Profile input is unavailable; provide --profile explicitly.", 3)
	}
	file, ok := streams.In.(*os.File)
	if !ok {
		return "", failure()
	}
	handle := windows.Handle(file.Fd())
	var previous uint32
	if windows.GetConsoleMode(handle, &previous) != nil {
		return "", failure()
	}
	visible := (previous | windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_EXTENDED_FLAGS) &^ (windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_QUICK_EDIT_MODE)
	if windows.SetConsoleMode(handle, visible) != nil {
		return "", failure()
	}
	defer func() {
		if result != nil {
			_, _, _ = flushConsoleInput.Call(uintptr(handle))
		}
		if windows.SetConsoleMode(handle, previous) != nil {
			value, result = "", failure()
		}
	}()
	if _, err := io.WriteString(streams.Err, prompt); err != nil {
		return "", initInputError("io_error", "Cannot write profile prompt.", 1)
	}
	units := make([]uint16, 0, 129)
	for {
		if failure := protocol.ContextError(ctx.Err()); failure != nil {
			return "", failure
		}
		var event consoleInputRecord
		var count uint32
		if consoleRecord(peekConsoleInput, handle, &event, &count) != nil {
			return "", failure()
		}
		if count == 0 {
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		if consoleRecord(readConsoleInput, handle, &event, &count) != nil {
			return "", failure()
		}
		if count == 0 || event.kind != 1 || event.down == 0 {
			continue
		}
		switch event.character {
		case 3:
			return "", initInputError("canceled", "Project initialization canceled.", 130)
		case 4, 26:
			return "", failure()
		case 10, 13:
			if _, err := io.WriteString(streams.Err, "\n"); err != nil {
				return "", failure()
			}
			return string(utf16.Decode(units)), nil
		case 8:
			for n := uint16(0); n < event.repeat && len(units) > 0; n++ {
				units = units[:len(units)-1]
				_, _ = io.WriteString(streams.Err, "\b \b")
			}
		case 0:
		default:
			for n := uint16(0); n < event.repeat; n++ {
				if len(units) < 129 {
					units = append(units, event.character)
				}
				// Profile input is public. Render control characters as a visible
				// replacement while retaining them for validation.
				shown := event.character
				if shown < 32 || shown == 127 {
					shown = 0xfffd
				}
				if _, err := io.WriteString(streams.Err, string(rune(shown))); err != nil {
					return "", failure()
				}
			}
		}
	}
}
