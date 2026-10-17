package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "__w4d-launch":
			var record Record
			if len(os.Args) != 4 || json.Unmarshal([]byte(os.Args[3]), &record) != nil {
				os.Exit(96)
			}
			store := Store{Data: os.Args[2]}
			lease, failure := store.prepareLaunch(record)
			if failure != nil {
				fmt.Fprintln(os.Stderr, failure)
				os.Exit(97)
			}
			exe, _ := os.Executable()
			cmd := exec.Command(exe, "__process-serve", store.Data, record.ID, "3")
			if err := startSupervisor(cmd, lease.file); err != nil {
				lease.closeReference()
				fmt.Fprintln(os.Stderr, err)
				os.Exit(98)
			}
			lease.closeReference()
			fmt.Println(cmd.Process.Pid)
			os.Exit(0)
		case "__process-serve":
			if len(os.Args) != 5 {
				os.Exit(90)
			}
			lease, err := OpenInheritedLease(os.Args[4])
			if err != nil {
				os.Exit(91)
			}
			defer lease.Close()
			err = (Store{Data: os.Args[2]}).ServeWithLease(context.Background(), os.Args[3], lease,
				func(ctx context.Context, r Record, runner process.Runner, output io.Writer) *protocol.Error {
					exe, _ := os.Executable()
					ctx = WithEnvironmentSelection(ctx, "actual", true)
					probe := &project.ReadyProbe{Exec: []string{exe, "__w4d-probe"}, Timeout: "5s"}
					if r.Command == "probe-tree" {
						probe = &project.ReadyProbe{Exec: []string{exe, "__w4d-probe-tree", r.Directory}, Timeout: "1s"}
					}
					ctx = process.WithReadyProbe(ctx, probe)
					args := []string{exe, "__w4d-app", r.Directory, r.Command}
					if r.Command == "missing" {
						args = []string{filepath.Join(r.Directory, "absent.exe")}
					}
					_, failure := runner(ctx, args, r.Directory, os.Environ(), nil, output, output)
					return failure
				})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(92)
			}
			os.Exit(0)
		case "__w4d-probe-tree", "__w4d-probe-leaf":
			signal.Ignore(os.Interrupt)
			name := "probe-root.pid"
			if os.Args[1] == "__w4d-probe-leaf" {
				name = "probe-child.pid"
			}
			if os.WriteFile(filepath.Join(os.Args[2], name), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
				os.Exit(93)
			}
			if os.Args[1] == "__w4d-probe-tree" {
				exe, _ := os.Executable()
				child := exec.Command(exe, "__w4d-probe-leaf", os.Args[2])
				child.Stdout = os.Stdout
				child.Stderr = os.Stderr
				if child.Start() != nil {
					os.Exit(94)
				}
				go child.Wait()
			}
			for {
				time.Sleep(time.Hour)
			}
		case "__w4d-probe":
			os.Exit(0)
		case "__w4d-app", "__w4d-child":
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt)
			if err := os.WriteFile(filepath.Join(os.Args[2], os.Args[1]+".pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
				os.Exit(93)
			}
			if os.Args[1] == "__w4d-app" {
				exe, _ := os.Executable()
				child := exec.Command(exe, "__w4d-child", os.Args[2], os.Args[3])
				child.Stdout = os.Stdout
				child.Stderr = os.Stderr
				if child.Start() != nil {
					os.Exit(94)
				}
				go child.Wait()
				fmt.Println("native supervisor output")
				if os.Args[3] == "early" {
					time.Sleep(300 * time.Millisecond)
					os.Exit(23)
				}
			}
			if os.Args[3] == "ignore" {
				for {
					time.Sleep(time.Hour)
				}
			}
			<-signals
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func waitWindowsCondition(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("native supervisor condition timed out")
}
func windowsObservedProcess(t *testing.T, path string) windows.Handle {
	t.Helper()
	var handle windows.Handle
	waitWindowsCondition(t, func() bool {
		b, e := os.ReadFile(path)
		if e != nil {
			return false
		}
		pid, e := strconv.ParseUint(string(b), 10, 32)
		if e != nil {
			t.Fatal(e)
		}
		handle, e = windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if e != nil {
			t.Fatal(e)
		}
		return true
	})
	t.Cleanup(func() { windows.CloseHandle(handle) })
	return handle
}
func assertWindowsProcessExited(t *testing.T, handle windows.Handle) {
	t.Helper()
	result, err := windows.WaitForSingleObject(handle, 10000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant survived: %d %v", result, err)
	}
}
func launchWindowsSupervisor(t *testing.T, mode string) (Store, Record, *exec.Cmd) {
	t.Helper()
	store, record := windowsStartingRecord(t)
	record.Command = mode
	record.Capture = true
	lease, failure := store.prepareLaunch(record)
	if failure != nil {
		t.Fatal(failure)
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "__process-serve", store.Data, record.ID, "3")
	if err := startSupervisor(cmd, lease.file); err != nil {
		lease.closeReference()
		t.Fatal(err)
	}
	lease.closeReference()
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return store, record, cmd
}

func TestWindowsSupervisorStopReadinessLogsAndAuthorization(t *testing.T) {
	store, record, cmd := launchWindowsSupervisor(t, "grace")
	root := windowsObservedProcess(t, filepath.Join(record.Directory, "__w4d-app.pid"))
	child := windowsObservedProcess(t, filepath.Join(record.Directory, "__w4d-child.pid"))
	var current Record
	waitWindowsCondition(t, func() bool {
		r, e := store.Status(context.Background(), record.ID)
		current = r
		return e == nil && r.State == "running"
	})
	if current.Env != "actual" || current.Inject == nil || !*current.Inject || current.StartedAt == nil {
		t.Fatalf("actual start selection missing: %+v", current)
	}
	checked, failure := store.Check(context.Background(), record.ID)
	if failure != nil || checked.Readiness == nil || !checked.Readiness.Ready {
		t.Fatalf("readiness: %+v %v", checked, failure)
	}
	logs, failure := store.Logs(context.Background(), record.ID)
	if failure != nil || !strings.Contains(logs, "native supervisor output") {
		t.Fatalf("logs: %q %v", logs, failure)
	}
	var control control
	if err := tasks.ReadPrivate(store.path(record.ID, "control.json"), &control); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"token", "origin", "host", "method"} {
		req, _ := http.NewRequest("POST", control.Address+"/stop", nil)
		req.Header.Set("Authorization", "Bearer "+control.Token)
		switch kind {
		case "token":
			req.Header.Set("Authorization", "Bearer wrong")
		case "origin":
			req.Header.Set("Origin", "http://outside.invalid")
		case "host":
			req.Host = "outside.invalid"
		case "method":
			req.Method = "GET"
		}
		client := http.Client{Timeout: time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("%s control authorized: %d", kind, resp.StatusCode)
		}
	}
	if _, err := store.rpc(context.Background(), record.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	assertWindowsProcessExited(t, root)
	assertWindowsProcessExited(t, child)
	waitWindowsCondition(t, func() bool { r, e := store.read(record.ID); current = r; return e == nil && r.EndedAt != nil })
	if current.State != "stopped" || current.Reason != "stop_requested" || current.ExitCode == nil || *current.ExitCode != 130 {
		t.Fatalf("stop record: %+v", current)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	available, failure := store.leaseAvailable(context.Background(), record.ID, false)
	if failure != nil || !available {
		t.Fatalf("lease retained: %v %v", available, failure)
	}
}

func TestWindowsSupervisorForcedStopAndCrash(t *testing.T) {
	for _, mode := range []string{"ignore", "crash"} {
		t.Run(mode, func(t *testing.T) {
			store, record, cmd := launchWindowsSupervisor(t, mode)
			root := windowsObservedProcess(t, filepath.Join(record.Directory, "__w4d-app.pid"))
			child := windowsObservedProcess(t, filepath.Join(record.Directory, "__w4d-child.pid"))
			waitWindowsCondition(t, func() bool {
				r, e := store.Status(context.Background(), record.ID)
				return e == nil && r.State == "running"
			})
			if mode == "crash" {
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
				cmd.Wait()
			} else {
				if _, e := store.rpc(context.Background(), record.ID, "stop"); e != nil {
					t.Fatal(e)
				}
				if err := cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			assertWindowsProcessExited(t, root)
			assertWindowsProcessExited(t, child)
			current, e := store.Status(context.Background(), record.ID)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "crash" {
				if current.State != "interrupted" {
					t.Fatalf("crash record: %+v", current)
				}
			} else if current.ExitCode == nil || *current.ExitCode != 137 {
				t.Fatalf("forced record: %+v", current)
			}
		})
	}
}

func TestWindowsSupervisorEarlyExitAndLaunchFailure(t *testing.T) {
	for _, mode := range []string{"early", "missing"} {
		t.Run(mode, func(t *testing.T) {
			store, record, cmd := launchWindowsSupervisor(t, mode)
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			current, failure := store.Status(context.Background(), record.ID)
			if failure != nil {
				t.Fatal(failure)
			}
			if mode == "missing" {
				if current.State != "failed" || current.Reason != "command_not_found" || current.StartedAt != nil {
					t.Fatalf("failed start publication: %+v", current)
				}
			} else {
				if current.State != "exited" || current.ExitCode == nil || *current.ExitCode != 23 {
					b, _ := json.Marshal(current)
					t.Fatalf("early exit: %s", b)
				}
				b, err := os.ReadFile(filepath.Join(record.Directory, "__w4d-child.pid"))
				if err != nil {
					t.Fatal(err)
				}
				pid, _ := strconv.ParseUint(string(b), 10, 32)
				h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
				if err == nil {
					defer windows.CloseHandle(h)
					assertWindowsProcessExited(t, h)
				}
			}
		})
	}
}

func TestWindowsSupervisorSurvivesLaunchingCLIExit(t *testing.T) {
	store, record := windowsStartingRecord(t)
	record.Command = "grace"
	record.Capture = true
	body, _ := json.Marshal(record)
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, exe, "__w4d-launch", store.Data, string(body)).Output()
	if err != nil {
		t.Fatalf("launcher: %s %v", output, err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.TerminateProcess(supervisor, 99); windows.CloseHandle(supervisor) })
	root := windowsObservedProcess(t, filepath.Join(record.Directory, "__w4d-app.pid"))
	child := windowsObservedProcess(t, filepath.Join(record.Directory, "__w4d-child.pid"))
	waitWindowsCondition(t, func() bool { r, e := store.Status(ctx, record.ID); return e == nil && r.State == "running" })
	if _, err := store.rpc(ctx, record.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	assertWindowsProcessExited(t, root)
	assertWindowsProcessExited(t, child)
	assertWindowsProcessExited(t, supervisor)
	available, failure := store.leaseAvailable(ctx, record.ID, false)
	if failure != nil || !available {
		t.Fatalf("launch exit lost lease: %v %v", available, failure)
	}
}

func TestWindowsSupervisorHandoffRejectsClosedFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "__process-serve", "unused", "unused", "3")
	if err := startSupervisor(cmd, file); err == nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("closed file adopted as pseudo process handle")
	}
	for _, argument := range []string{"0", "-1", "18446744073709551615", "3x", ""} {
		if file, err := OpenInheritedLease(argument); err == nil {
			file.Close()
			t.Fatalf("invalid handoff %q accepted", argument)
		}
	}
}

func TestWindowsSupervisorReadinessTimeoutReapsProbeTreeOnly(t *testing.T) {
	store, record, cmd := launchWindowsSupervisor(t, "probe-tree")
	app := windowsObservedProcess(t, filepath.Join(record.Directory, "__w4d-app.pid"))
	waitWindowsCondition(t, func() bool {
		r, e := store.Status(context.Background(), record.ID)
		return e == nil && r.State == "running"
	})
	type result struct {
		record  Record
		failure *protocol.Error
	}
	done := make(chan result, 1)
	go func() { r, e := store.Check(context.Background(), record.ID); done <- result{r, e} }()
	root := windowsObservedProcess(t, filepath.Join(record.Directory, "probe-root.pid"))
	child := windowsObservedProcess(t, filepath.Join(record.Directory, "probe-child.pid"))
	select {
	case result := <-done:
		if result.failure != nil || result.record.Readiness == nil || result.record.Readiness.Ready || result.record.Readiness.Reason != "probe_timeout" {
			t.Fatalf("probe timeout: %+v %v", result.record, result.failure)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("probe timeout did not finish")
	}
	assertWindowsProcessExited(t, root)
	assertWindowsProcessExited(t, child)
	state, err := windows.WaitForSingleObject(app, 0)
	if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("probe timeout stopped application: %d %v", state, err)
	}
	if _, err := store.rpc(context.Background(), record.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	assertWindowsProcessExited(t, app)
}
