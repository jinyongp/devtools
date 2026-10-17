package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

type consolePromptWriter struct {
	run   func()
	wrote bool
}

func (w *consolePromptWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.wrote = true
		w.run()
	}
	return len(p), nil
}

type failingConsoleWriter struct{}

func (failingConsoleWriter) Write([]byte) (int, error) { return 0, errors.New("fixture") }

func TestWindowsConsoleChild(t *testing.T) {
	scenario := os.Getenv("DEVTOOLS_CONSOLE_FIXTURE")
	if scenario == "" {
		return
	}
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := windows.Handle(f.Fd())
	var before uint32
	if err = windows.GetConsoleMode(h, &before); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	writer := &consolePromptWriter{run: func() {
		var quiet uint32
		if windows.GetConsoleMode(h, &quiet) != nil || quiet&windows.ENABLE_ECHO_INPUT != 0 || quiet&windows.ENABLE_LINE_INPUT != 0 {
			t.Fatal("prompt exposed before quiet console")
		}
		var chars []uint16
		switch scenario {
		case "success":
			chars = utf16.Encode([]rune("한글😀\bX\r"))
		case "ctrl-c":
			chars = []uint16{3}
		case "eof":
			chars = []uint16{26}
		case "cancel":
			cancel()
			return
		case "timeout":
			return
		case "invalid-unicode":
			chars = []uint16{0xd800, 13}
		default:
			t.Fatal("unknown scenario")
		}
		records := make([]consoleInputRecord, len(chars))
		for i, c := range chars {
			records[i] = consoleInputRecord{kind: 1, down: 1, repeat: 1, character: c}
		}
		var count uint32
		ok, _, err := consoleKernel.NewProc("WriteConsoleInputW").Call(uintptr(h), uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&count)))
		if ok == 0 || int(count) != len(records) {
			t.Fatal("inject failed", err)
		}
	}}
	var sink io.Writer = writer
	if scenario == "writer-error" {
		sink = failingConsoleWriter{}
	}
	value, e := readTerminalPassphrase(ctx, IO{In: f, Err: sink}, "Passphrase: ")
	var after uint32
	if windows.GetConsoleMode(h, &after) != nil || before != after {
		t.Fatal("console mode not restored")
	}
	switch scenario {
	case "success":
		if e != nil || value != "한글X" {
			t.Fatal("console input contract failed")
		}
	case "cancel", "timeout", "ctrl-c":
		if e == nil || e.Code != "canceled" || value != "" {
			t.Fatal("cancellation contract failed")
		}
	default:
		if e == nil || e.Code != "io_error" || value != "" {
			t.Fatal("failure contract failed")
		}
	}
}

func TestWindowsConsoleInputRestoresMode(t *testing.T) {
	for _, scenario := range []string{"success", "ctrl-c", "cancel", "timeout", "eof", "writer-error", "invalid-unicode"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(ctx, binary, "-test.run=^TestWindowsConsoleChild$", "-test.v")
			command.Env = append(os.Environ(), "DEVTOOLS_CONSOLE_FIXTURE="+scenario)
			command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
			var out bytes.Buffer
			command.Stdout = &out
			command.Stderr = &out
			if err = command.Run(); err != nil {
				t.Fatalf("console fixture failed: %v %s", err, out.String())
			}
		})
	}
}

func TestWindowsPassphrasePipeAndCancellation(t *testing.T) {
	value, e := readPassphraseReader(context.Background(), strings.NewReader("한글 secret\r\n"))
	if e != nil || value != "한글 secret" {
		t.Fatal("pipe normalization failed")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	value, e = readPassphraseReader(ctx, r)
	if e == nil || e.Code != "canceled" || value != "" || time.Since(start) > time.Second {
		t.Fatal("blocked pipe cancellation")
	}
	if _, err = r.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatal("canceled input handle remains open")
	}
	if _, e = readTerminalPassphrase(context.Background(), IO{In: strings.NewReader("secret")}, "Prompt"); e == nil || e.Code != "passphrase_required" {
		t.Fatal("redirected stdin prompted")
	}
}
