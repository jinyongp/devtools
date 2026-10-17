package platformfs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsLockChild(t *testing.T) {
	mode := os.Getenv("DEVTOOLS_LOCK_CHILD")
	if mode == "" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) < 1 {
		os.Exit(90)
	}
	if mode == "lease" {
		if len(args) != 2 {
			os.Exit(91)
		}
		number, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			os.Exit(92)
		}
		incoming := os.NewFile(uintptr(number), "inherited-lease")
		root, err := OpenRoot(filepath.Dir(args[1]), false)
		if err != nil {
			os.Exit(93)
		}
		file, err := root.AdoptFile(filepath.Base(args[1]), incoming)
		if err != nil {
			os.Exit(94)
		}
		var flags uint32
		getFlags := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetHandleInformation")
		ok, _, _ := getFlags.Call(file.Fd(), uintptr(unsafe.Pointer(&flags)))
		if ok == 0 || flags&windows.HANDLE_FLAG_INHERIT != 0 {
			os.Exit(95)
		}
		fmt.Fprintln(os.Stdout, "held")
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = file.Close()
		_ = root.Close()
		os.Exit(0)
	}
	root, err := OpenRoot(filepath.Dir(args[0]), false)
	if err != nil {
		os.Exit(96)
	}
	f, err := root.OpenFile(filepath.Base(args[0]), os.O_RDWR)
	if err != nil {
		os.Exit(97)
	}
	acquired, err := f.TryLock(mode == "shared")
	if err != nil {
		os.Exit(98)
	}
	if acquired {
		fmt.Fprint(os.Stdout, "acquired")
		_ = f.Unlock()
	} else {
		fmt.Fprint(os.Stdout, "busy")
	}
	_ = f.Close()
	_ = root.Close()
	os.Exit(0)
}
func TestWindowsSharedExclusiveCrossProcess(t *testing.T) {
	root := testRoot(t)
	file, err := root.OpenFile("rw.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	acquired, err := file.TryLock(true)
	if err != nil || !acquired {
		t.Fatalf("shared lock: %v", err)
	}
	defer file.Unlock()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for mode, want := range map[string]string{"shared": "acquired", "exclusive": "busy"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, exe, "-test.run=TestWindowsLockChild", "--", file.Name())
		cmd.Env = append(os.Environ(), "DEVTOOLS_LOCK_CHILD="+mode)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil || string(out) != want {
			t.Fatalf("%s: %q %v", mode, out, err)
		}
	}
}
func TestWindowsExclusiveReferenceSurvivesParentClose(t *testing.T) {
	root := testRoot(t)
	lease, err := root.OpenExclusive("lease", true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=TestWindowsLockChild", "--", strconv.FormatUint(uint64(lease.Fd()), 10), lease.Name())
	cmd.Env = append(os.Environ(), "DEVTOOLS_LOCK_CHILD=lease")
	cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(lease.Fd())}}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic strings.Builder
	cmd.Stderr = &diagnostic
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || strings.TrimSpace(ready) != "held" {
		t.Fatalf("child handoff: %q %v %s", ready, err, &diagnostic)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := root.OpenExclusive("lease", false, false)
	if err == nil {
		other.Close()
		t.Fatal("parent close opened an exclusion gap")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("lease probe: %v", err)
	}
	if err = input.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("child exit: %v %s", err, &diagnostic)
	}
	after, err := root.OpenExclusive("lease", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = after.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(lease.Name()); err != nil {
		t.Fatal("empty retained lease file was removed")
	}
}
func TestWindowsAdoptRejectsWrongIdentityWithoutClosingInput(t *testing.T) {
	root := testRoot(t)
	lease, err := root.OpenExclusive("lease", true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err = root.WriteFile(context.Background(), "other", nil, false); err != nil {
		t.Fatal(err)
	}
	if file, err := root.AdoptFile("other", lease.File); err == nil {
		file.Close()
		t.Fatal("wrong inherited file identity accepted")
	}
	other, err := root.OpenExclusive("lease", false, false)
	if err == nil {
		other.Close()
		t.Fatal("failed adoption closed the input lease")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatal(err)
	}
}
func TestWindowsLeaseOutsideStageAllowsDirectoryPublication(t *testing.T) {
	root := testRoot(t)
	if err := root.MkdirAll("process-leases"); err != nil {
		t.Fatal(err)
	}
	if err := root.MkdirAll(filepath.Join("processes", "staging")); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(context.Background(), filepath.Join("processes", "staging", "record.json"), []byte("complete"), false); err != nil {
		t.Fatal(err)
	}
	lease, err := root.OpenExclusive(filepath.Join("process-leases", "id.lock"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err = root.PublishDirectory(context.Background(), filepath.Join("processes", "staging"), filepath.Join("processes", "id")); err != nil {
		t.Fatal(err)
	}
	data, err := root.ReadFile(filepath.Join("processes", "id", "record.json"), 100)
	if err != nil || string(data) != "complete" {
		t.Fatalf("publication: %q %v", data, err)
	}
	if err = root.MkdirAll(filepath.Join("processes", "next")); err != nil {
		t.Fatal(err)
	}
	if err = root.PublishDirectory(context.Background(), filepath.Join("processes", "next"), filepath.Join("processes", "id")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing execution overwritten: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root.path, "processes", "next")); err != nil {
		t.Fatal("failed publication removed recoverable stage")
	}
}

func TestWindowsInvalidLockHandleIsNotContention(t *testing.T) {
	root := testRoot(t)
	file, err := root.OpenFile("lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	acquired, err := file.TryLock(false)
	if acquired || err == nil {
		t.Fatal("invalid handle was hidden as lock contention")
	}
}
