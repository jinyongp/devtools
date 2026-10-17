package truststore

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Only disposable-runner tests automate OS consent. Product code never clicks a
// dialog. Match our PID, the exact unique fixture subject, dialog class and the
// expected Yes/No controls; never interact with a foreign or unrecognized UI.
func approveFixtureRootDialogs(t *testing.T, subject string) {
	t.Helper()
	if os.Getenv("DEVTOOLS_TEST_SYSTEM_NETWORK") != "1" || !strings.HasPrefix(subject, "devtools local root ") {
		t.Fatal("unscoped certificate dialog fixture")
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	textProc := user32.NewProc("GetWindowTextW")
	classProc := user32.NewProc("GetClassNameW")
	pidProc := user32.NewProc("GetWindowThreadProcessId")
	childrenProc := user32.NewProc("EnumChildWindows")
	dialogProc := user32.NewProc("GetDlgItem")
	postProc := user32.NewProc("PostMessageW")
	text := func(window uintptr) string {
		buffer := make([]uint16, 4096)
		n, _, _ := textProc.Call(window, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		if n >= uintptr(len(buffer)) {
			return ""
		}
		return windows.UTF16ToString(buffer[:n])
	}
	var content strings.Builder
	childCallback := syscall.NewCallback(func(child, parameter uintptr) uintptr {
		if content.Len() < 65536 {
			content.WriteString(text(child))
			content.WriteByte('\n')
		}
		return 1
	})
	callback := syscall.NewCallback(func(window, parameter uintptr) uintptr {
		var pid uint32
		pidProc.Call(window, uintptr(unsafe.Pointer(&pid)))
		if pid != uint32(os.Getpid()) {
			return 1
		}
		class := make([]uint16, 64)
		classProc.Call(window, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if windows.UTF16ToString(class) != "#32770" {
			return 1
		}
		content.Reset()
		childrenProc.Call(window, childCallback, 0)
		if !strings.Contains(content.String(), subject) {
			return 1
		}
		yes, _, _ := dialogProc.Call(window, 6)
		no, _, _ := dialogProc.Call(window, 7)
		if yes == 0 || no == 0 || strings.ReplaceAll(text(yes), "&", "") != "Yes" || strings.ReplaceAll(text(no), "&", "") != "No" {
			return 1
		}
		ok, _, _ := postProc.Call(yes, 0x00f5, 0, 0) // BM_CLICK, asynchronously on its owning UI thread.
		if ok != 0 {
			t.Log("approved disposable CurrentUser root dialog for exact fixture subject")
		}
		return 1
	})
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				user32.NewProc("EnumWindows").Call(callback, 0)
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
}
