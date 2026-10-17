package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"
)

func jobFixtureEnvironment(dir, mode string, depth int) []string {
	return Environment(os.Environ(), map[string]string{"DEVTOOLS_JOB_NODE": "1", "DEVTOOLS_JOB_DIR": dir, "DEVTOOLS_JOB_MODE": mode, "DEVTOOLS_JOB_DEPTH": strconv.Itoa(depth)})
}
func TestWindowsJobNode(t *testing.T) {
	if os.Getenv("DEVTOOLS_JOB_NODE") != "1" {
		return
	}
	dir, mode := os.Getenv("DEVTOOLS_JOB_DIR"), os.Getenv("DEVTOOLS_JOB_MODE")
	depth, err := strconv.Atoi(os.Getenv("DEVTOOLS_JOB_DEPTH"))
	if err != nil {
		os.Exit(80)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	if depth > 0 {
		exe, err := os.Executable()
		if err != nil {
			os.Exit(81)
		}
		cmd := exec.Command(exe, "-test.run=^TestWindowsJobNode$")
		cmd.Env = jobFixtureEnvironment(dir, mode, depth-1)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err = cmd.Start(); err != nil {
			os.Exit(82)
		}
		go func() { _ = cmd.Wait() }()
	}
	temporary := filepath.Join(dir, fmt.Sprintf("node-%d.tmp", depth))
	if err = os.WriteFile(temporary, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.Exit(83)
	}
	if err = os.Rename(temporary, filepath.Join(dir, fmt.Sprintf("node-%d", depth))); err != nil {
		os.Exit(83)
	}
	if mode == "early" && depth == 2 {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(dir, "release-root")); err == nil {
				os.Exit(23)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(84)
	}
	for {
		select {
		case <-signals:
			if mode == "grace" {
				os.Exit(0)
			}
		case <-time.After(time.Minute):
			os.Exit(85)
		}
	}
}

type jobScenarioResult struct {
	Code       int
	Error      string
	Nodes      int
	StopMillis int64
}

func openFixtureNodes(dir string, count int) ([]windows.Handle, error) {
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		complete := true
		for n := 0; n < count; n++ {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("node-%d", n))); err != nil {
				complete = false
				break
			}
		}
		if complete {
			var handles []windows.Handle
			for n := 0; n < count; n++ {
				body, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("node-%d", n)))
				if err != nil {
					for _, h := range handles {
						windows.CloseHandle(h)
					}
					return nil, err
				}
				pid, err := strconv.ParseUint(string(body), 10, 32)
				if err != nil {
					closeFixtureNodes(handles)
					return nil, err
				}
				h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
				if err != nil {
					for _, h := range handles {
						windows.CloseHandle(h)
					}
					return nil, err
				}
				handles = append(handles, h)
			}
			return handles, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, errors.New("fixture tree was not ready")
}
func closeFixtureNodes(handles []windows.Handle) {
	for _, h := range handles {
		_ = windows.CloseHandle(h)
	}
}
func waitFixtureNodes(handles []windows.Handle) error {
	for _, h := range handles {
		state, err := windows.WaitForSingleObject(h, 5000)
		if err != nil {
			return err
		}
		if state != windows.WAIT_OBJECT_0 {
			return errors.New("owned descendant survived process/job shutdown")
		}
	}
	return nil
}
func TestWindowsJobConsoleFixture(t *testing.T) {
	mode := os.Getenv("DEVTOOLS_JOB_SCENARIO")
	if mode == "" {
		return
	}
	result := func() jobScenarioResult {
		exe, err := os.Executable()
		if err != nil {
			return jobScenarioResult{Error: err.Error()}
		}
		dir := os.Getenv("DEVTOOLS_JOB_DIR")
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		done := make(chan jobScenarioResult, 1)
		go func() {
			code, e := Execute(ctx, []string{exe, "-test.run=^TestWindowsJobNode$"}, dir, jobFixtureEnvironment(dir, mode, 2), nil, io.Discard, io.Discard)
			r := jobScenarioResult{Code: code}
			if e != nil {
				r.Error = e.Code
			}
			done <- r
		}()
		handles, err := openFixtureNodes(dir, 3)
		if err != nil {
			return jobScenarioResult{Error: err.Error()}
		}
		defer closeFixtureNodes(handles)
		start := time.Now()
		if mode == "early" {
			if err = os.WriteFile(filepath.Join(dir, "release-root"), nil, 0600); err != nil {
				return jobScenarioResult{Error: err.Error()}
			}
		} else {
			cancel()
		}
		r := <-done
		r.StopMillis = time.Since(start).Milliseconds()
		r.Nodes = len(handles)
		if err = waitFixtureNodes(handles); err != nil {
			r.Error = err.Error()
		}
		return r
	}()
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if result.Error != "" {
		os.Exit(1)
	}
	os.Exit(0)
}
func runJobConsoleScenario(t *testing.T, mode string) jobScenarioResult {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestWindowsJobConsoleFixture$")
	cmd.Env = Environment(os.Environ(), map[string]string{"DEVTOOLS_JOB_SCENARIO": mode, "DEVTOOLS_JOB_DIR": dir})
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	if err = cmd.Run(); err != nil {
		t.Fatalf("console fixture: %v out=%q err=%q", err, out.String(), diagnostic.String())
	}
	var result jobScenarioResult
	if err = json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err, out.String())
	}
	if result.Error != "" || result.Nodes != 3 {
		t.Fatal(result)
	}
	return result
}
func TestWindowsJobGracefulCancellationReapsWholeTree(t *testing.T) {
	result := runJobConsoleScenario(t, "grace")
	if result.Code != 130 || result.StopMillis >= 2800 {
		t.Fatalf("graceful cancellation: %#v", result)
	}
}
func TestWindowsJobForceCancellationReapsIgnoringTree(t *testing.T) {
	result := runJobConsoleScenario(t, "ignore")
	if result.Code != 137 || result.StopMillis < 2800 || result.StopMillis > 7000 {
		t.Fatalf("forced cancellation: %#v", result)
	}
}
func TestWindowsJobNormalEarlyExitReapsDescendantsAndPipes(t *testing.T) {
	result := runJobConsoleScenario(t, "early")
	if result.Code != 23 || result.StopMillis >= 5000 {
		t.Fatalf("normal exit/tree/EOF: %#v", result)
	}
}
func TestWindowsJobCrashOwnerFixture(t *testing.T) {
	if os.Getenv("DEVTOOLS_JOB_CRASH_OWNER") != "1" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(86)
	}
	dir := os.Getenv("DEVTOOLS_JOB_DIR")
	command, e := prepareInvocation(context.Background(), []string{exe, "-test.run=^TestWindowsJobNode$"}, dir, jobFixtureEnvironment(dir, "ignore", 2))
	if e != nil {
		os.Exit(87)
	}
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	child, err := startNativeOwned(context.Background(), command)
	if err != nil {
		os.Exit(88)
	}
	active, err := child.activeProcesses()
	if err != nil || active < 1 {
		os.Exit(89)
	}
	if err = os.WriteFile(filepath.Join(dir, "owner-ready"), nil, 0600); err != nil {
		os.Exit(90)
	}
	time.Sleep(time.Hour)
	os.Exit(91)
}
func TestWindowsJobParentCrashClosesOnlyJobHandle(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := exec.CommandContext(ctx, exe, "-test.run=^TestWindowsJobCrashOwnerFixture$")
	owner.Env = Environment(os.Environ(), map[string]string{"DEVTOOLS_JOB_CRASH_OWNER": "1", "DEVTOOLS_JOB_DIR": dir})
	owner.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE, HideWindow: true}
	var diagnostic bytes.Buffer
	owner.Stderr = &diagnostic
	if err = owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	handles, err := openFixtureNodes(dir, 3)
	if err != nil {
		_ = owner.Process.Kill()
		_ = owner.Wait()
		t.Fatal(err, diagnostic.String())
	}
	defer closeFixtureNodes(handles)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(dir, "owner-ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner-ready marker missing", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err = owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	if err = waitFixtureNodes(handles); err != nil {
		t.Fatal(err)
	}
}
func windowsHandleCount(t *testing.T) uint32 {
	t.Helper()
	var count uint32
	result, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount").Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)))
	if result == 0 {
		t.Fatal(err)
	}
	return count
}
func TestWindowsOwnedLaunchFailureClosesHandles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.exe")
	if err := os.WriteFile(path, []byte("invalid executable"), 0600); err != nil {
		t.Fatal(err)
	}
	attempt := func() {
		code, e := Execute(context.Background(), []string{path}, dir, os.Environ(), strings.NewReader("input"), io.Discard, io.Discard)
		if e == nil || e.Code != "execution_failed" || code != 0 {
			t.Fatalf("invalid image: %d %v", code, e)
		}
	}
	attempt()
	before := windowsHandleCount(t)
	for range 20 {
		attempt()
	}
	after := windowsHandleCount(t)
	if after > before+2 {
		t.Fatalf("failed starts leaked handles: before=%d after=%d", before, after)
	}
}
func TestWindowsClosedStdioCannotInheritPseudoHandle(t *testing.T) {
	dir, exe := windowsChild(t)
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	input.Close()
	code, e := Execute(context.Background(), []string{exe, "-test.run=^TestWindowsArgvChild$"}, dir, Environment(os.Environ(), map[string]string{"DEVTOOLS_ARGV_CHILD": "streams"}), input, io.Discard, io.Discard)
	if e == nil || e.Code != "execution_failed" || code != 0 {
		t.Fatalf("closed input accepted as a pseudo handle: %d %v", code, e)
	}
}

func TestWindowsJobHandleFixture(t *testing.T) {
	if os.Getenv("DEVTOOLS_JOB_HANDLE_TEST") != "1" {
		return
	}
	value, err := strconv.ParseUint(os.Getenv("DEVTOOLS_JOB_TEST_HANDLE"), 10, 64)
	if err != nil {
		os.Exit(94)
	}
	_ = windows.SetEvent(windows.Handle(value))
	os.Exit(0)
}
func TestWindowsOwnedHandleAllowlist(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	event, err := windows.CreateEvent(&attributes, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(event)
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_JOB_HANDLE_TEST": "1", "DEVTOOLS_JOB_TEST_HANDLE": strconv.FormatUint(uint64(event), 10)})
	code, failure := Execute(context.Background(), []string{exe, "-test.run=^TestWindowsJobHandleFixture$"}, dir, env, nil, io.Discard, io.Discard)
	if failure != nil || code != 0 {
		t.Fatalf("allowlist fixture: %d %v", code, failure)
	}
	if state, err := windows.WaitForSingleObject(event, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("unlisted inheritable event leaked: %d %v", state, err)
	}
}

func TestWindowsJobBreakawayFixture(t *testing.T) {
	if os.Getenv("DEVTOOLS_JOB_BREAKAWAY_TEST") != "1" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(95)
	}
	command := exec.Command(exe, "-test.run=^TestWindowsArgvChild$")
	command.Env = Environment(os.Environ(), map[string]string{"DEVTOOLS_ARGV_CHILD": "sleep"})
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB}
	err = command.Start()
	if err == nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		os.Exit(96)
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		os.Exit(97)
	}
	os.Exit(0)
}
func TestWindowsOwnedJobDisallowsBreakaway(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_JOB_BREAKAWAY_TEST": "1"})
	command, failure := prepareInvocation(context.Background(), []string{exe, "-test.run=^TestWindowsJobBreakawayFixture$"}, t.TempDir(), env)
	if failure != nil {
		t.Fatal(failure)
	}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	child, err := startNativeOwned(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err = windows.QueryInformationJobObject(child.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil); err != nil {
		child.closeJob()
		child.wait(context.Background())
		t.Fatal(err)
	}
	flags := limits.BasicLimitInformation.LimitFlags
	code, err := child.wait(context.Background())
	if err != nil || code != 0 {
		t.Fatalf("breakaway behavior: %d %v", code, err)
	}
	if flags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 || flags&(windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK|windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK) != 0 {
		t.Fatalf("unsafe owned job limit flags: %x", flags)
	}
}

func TestWindowsStartPublicationFailureReapsTree(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var handles []windows.Handle
	defer func() { closeFixtureNodes(handles) }()
	called := 0
	failure := protocol.NewError("io_error", "test start publication failed", 1, nil)
	code, result := ExecuteOwned(ctx, []string{exe, "-test.run=^TestWindowsJobNode$"}, dir, jobFixtureEnvironment(dir, "ignore", 2), nil, io.Discard, io.Discard, func() *protocol.Error {
		called++
		var openErr error
		handles, openErr = openFixtureNodes(dir, 3)
		if openErr != nil {
			return protocol.NewError("io_error", openErr.Error(), 1, nil)
		}
		return failure
	})
	if code != 0 || result != failure || called != 1 || len(handles) != 3 {
		t.Fatalf("start hook failure: code=%d error=%v calls=%d handles=%d", code, result, called, len(handles))
	}
	if err = waitFixtureNodes(handles); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsNativeEnvironmentKeepsCriticalRootAndOverrides(t *testing.T) {
	for _, test := range []struct {
		env  []string
		want string
	}{{[]string{"TOKEN=한글"}, os.Getenv("SYSTEMROOT")}, {[]string{"sYsTeMrOoT="}, ""}, {[]string{"SYSTEMROOT=selected"}, "selected"}} {
		block, err := nativeEnvironment(test.env)
		if err != nil {
			t.Fatal(err)
		}
		if len(block) < 2 || block[len(block)-1] != 0 || block[len(block)-2] != 0 {
			t.Fatal("environment block is not double-NUL terminated")
		}
		entries := strings.Split(strings.TrimRight(string(utf16.Decode(block)), "\x00"), "\x00")
		found := false
		for _, entry := range entries {
			key, value, ok := splitEnvironment(entry)
			if ok && strings.EqualFold(key, "SYSTEMROOT") {
				found = true
				if value != test.want {
					t.Fatalf("critical root override lost: %q", value)
				}
			}
		}
		if !found {
			t.Fatal("critical Windows environment missing")
		}
	}
	if _, err := nativeEnvironment([]string{"KEY=unsafe\x00value"}); err == nil {
		t.Fatal("NUL-containing environment accepted")
	}
}

func TestWindowsStartPublicationPanicReapsTree(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var handles []windows.Handle
	defer func() { closeFixtureNodes(handles) }()
	marker := errors.New("start publication panic fixture")
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = ExecuteOwned(ctx, []string{exe, "-test.run=^TestWindowsJobNode$"}, dir, jobFixtureEnvironment(dir, "ignore", 2), nil, io.Discard, io.Discard, func() *protocol.Error {
			var err error
			handles, err = openFixtureNodes(dir, 3)
			if err != nil {
				return protocol.NewError("io_error", err.Error(), 1, nil)
			}
			panic(marker)
		})
	}()
	if recovered != marker || len(handles) != 3 {
		t.Fatalf("start panic did not retain expected fixture: panic=%v handles=%d", recovered, len(handles))
	}
	if err = waitFixtureNodes(handles); err != nil {
		t.Fatal(err)
	}
}
