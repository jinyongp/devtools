package cli

import (
	"context"
	"io"
	"os"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

var consoleKernel = windows.NewLazySystemDLL("kernel32.dll")
var peekConsoleInput = consoleKernel.NewProc("PeekConsoleInputW")
var readConsoleInput = consoleKernel.NewProc("ReadConsoleInputW")
var flushConsoleInput = consoleKernel.NewProc("FlushConsoleInputBuffer")

// INPUT_RECORD contains a KEY_EVENT_RECORD at offset four on both architectures.
type consoleInputRecord struct {
	kind                         uint16
	padding                      uint16
	down                         int32
	repeat, key, scan, character uint16
	control                      uint32
}

func consoleRecord(proc *windows.LazyProc, handle windows.Handle, event *consoleInputRecord, count *uint32) error {
	ok, _, err := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(event)), 1, uintptr(unsafe.Pointer(count)))
	if ok == 0 {
		return err
	}
	return nil
}

func readTerminalPassphrase(ctx context.Context, streams IO, prompt string) (value string, result *protocol.Error) {
	canceled := func() *protocol.Error { return protocol.NewError("canceled", "Execution canceled.", 130, nil) }
	failure := func() *protocol.Error {
		return protocol.NewError("io_error", "Cannot read passphrase from console.", 1, nil)
	}
	if ctx.Err() != nil {
		return "", canceled()
	}
	file, ok := streams.In.(*os.File)
	if !ok {
		return "", passphraseRequiredError("import")
	}
	handle := windows.Handle(file.Fd())
	var previous uint32
	if windows.GetConsoleMode(handle, &previous) != nil {
		return "", passphraseRequiredError("import")
	}
	quiet := previous &^ (windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_QUICK_EDIT_MODE)
	quiet |= windows.ENABLE_EXTENDED_FLAGS
	if windows.SetConsoleMode(handle, quiet) != nil {
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
	if streams.Err != nil {
		if _, err := io.WriteString(streams.Err, prompt); err != nil {
			return "", failure()
		}
		defer func() { _, _ = io.WriteString(streams.Err, "\n") }()
	}
	units := make([]uint16, 0, transferPassphraseLimit)
	defer func() { clear(units) }()
	for {
		if ctx.Err() != nil {
			return "", canceled()
		}
		var event consoleInputRecord
		var count uint32
		if consoleRecord(peekConsoleInput, handle, &event, &count) != nil {
			return "", failure()
		}
		if count == 0 {
			// No goroutine or blocking console read survives cancellation. The prompt
			// owns stdin; concurrent readers on the same console are unsupported.
			select {
			case <-ctx.Done():
				return "", canceled()
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
			return "", canceled()
		case 26:
			return "", failure()
		case 13, 10:
			// utf16.Decode replaces unpaired surrogates; reject them explicitly instead.
			for i := 0; i < len(units); i++ {
				if units[i] >= 0xd800 && units[i] <= 0xdbff {
					if i+1 == len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
						return "", failure()
					}
					i++
				} else if units[i] >= 0xdc00 && units[i] <= 0xdfff {
					return "", failure()
				}
			}
			body := []byte(string(utf16.Decode(units)))
			defer clear(body)
			return normalizeTransferPassphrase(body)
		case 8:
			for n := uint16(0); n < event.repeat && len(units) > 0; n++ {
				last := units[len(units)-1]
				units[len(units)-1] = 0
				units = units[:len(units)-1]
				if last >= 0xdc00 && last <= 0xdfff && len(units) > 0 && units[len(units)-1] >= 0xd800 && units[len(units)-1] <= 0xdbff {
					units[len(units)-1] = 0
					units = units[:len(units)-1]
				}
			}
		case 0:
		default:
			if len(units)+int(event.repeat) > transferPassphraseLimit {
				return "", argumentError("Passphrase input is too large.", "")
			}
			for n := uint16(0); n < event.repeat; n++ {
				units = append(units, event.character)
			}
		}
	}
}
