//go:build ignore

// Standalone pre-implementation probe. Build this file explicitly from the repo
// root; it is deliberately excluded from the product and normal Go test graph.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type result struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

func ptr(s string) *uint16 { p, err := windows.UTF16PtrFromString(s); must(err); return p }
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func exclusive(path string, inherit bool) (windows.Handle, error) {
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{}))}
	if inherit {
		sa.InheritHandle = 1
	}
	return windows.CreateFile(ptr(path), windows.GENERIC_READ|windows.GENERIC_WRITE, 0, sa,
		windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
}

func child() bool {
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[1] {
	case "args":
		must(json.NewEncoder(os.Stdout).Encode(os.Args[2:]))
	case "lease":
		n, err := strconv.ParseUint(os.Args[2], 10, 64)
		must(err)
		h := windows.Handle(n)
		var info windows.ByHandleFileInformation
		must(windows.GetFileInformationByHandle(h, &info))
		must(windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, 0))
		fmt.Println("ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		must(windows.CloseHandle(h))
	case "lock":
		f, err := os.OpenFile(os.Args[2], os.O_RDWR, 0600)
		must(err)
		defer f.Close()
		flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
		if os.Args[3] == "exclusive" {
			flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
		}
		err = windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			fmt.Print("busy")
			break
		}
		must(err)
		fmt.Print("acquired")
		must(windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{}))
	case "sleep":
		time.Sleep(30 * time.Second)
	default:
		return false
	}
	return true
}

func main() {
	if child() {
		return
	}
	root, err := os.MkdirTemp("", "devtools-preflight-")
	must(err)
	defer os.RemoveAll(root)
	exe, err := os.Executable()
	must(err)
	results := []result{}
	check := func(name string, fn func() (bool, string)) {
		r := result{Name: name}
		func() {
			defer func() {
				if v := recover(); v != nil {
					r.Detail = fmt.Sprint(v)
				}
			}()
			r.Pass, r.Detail = fn()
		}()
		results = append(results, r)
	}
	check("native_arch", func() (bool, string) { return runtime.GOOS == "windows", runtime.GOOS + "/" + runtime.GOARCH })
	check("ntfs_fixture", func() (bool, string) {
		volume := make([]uint16, 1024)
		must(windows.GetVolumePathName(ptr(root), &volume[0], uint32(len(volume))))
		fs := make([]uint16, 64)
		must(windows.GetVolumeInformation(&volume[0], nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))))
		name := windows.UTF16ToString(fs)
		return name == "NTFS", name
	})
	check("lock_shared_exclusive", func() (bool, string) {
		path := filepath.Join(root, "rw.lock")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		must(err)
		defer f.Close()
		must(windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}))
		defer windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
		shared, err := exec.Command(exe, "lock", path, "shared").Output()
		must(err)
		exclusive, err := exec.Command(exe, "lock", path, "exclusive").Output()
		must(err)
		return string(shared) == "acquired" && string(exclusive) == "busy", "separate child processes"
	})
	check("lease_inherited_no_gap", func() (bool, string) {
		path := filepath.Join(root, "lease.lock")
		h, err := exclusive(path, true)
		must(err)
		defer func() {
			if h != 0 {
				_ = windows.CloseHandle(h)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "lease", strconv.FormatUint(uint64(h), 10))
		cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(h)}}
		in, err := cmd.StdinPipe()
		must(err)
		out, err := cmd.StdoutPipe()
		must(err)
		must(cmd.Start())
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		line, err := bufio.NewReader(out).ReadString('\n')
		must(err)
		must(windows.CloseHandle(h))
		h = 0
		other, busy := exclusive(path, false)
		if busy == nil {
			windows.CloseHandle(other)
		}
		// Metadata lookup remains possible without read/write access.
		meta, err := windows.CreateFile(ptr(path), 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		must(err)
		must(windows.CloseHandle(meta))
		must(in.Close())
		must(cmd.Wait())
		other, err = exclusive(path, false)
		must(err)
		must(windows.CloseHandle(other))
		return strings.TrimSpace(line) == "ready" && errors.Is(busy, windows.ERROR_SHARING_VIOLATION), "parent closed; child retained exclusive open; released on child exit"
	})
	check("private_acl_creation", func() (bool, string) {
		u, err := windows.GetCurrentProcessToken().GetTokenUser()
		must(err)
		sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + u.User.Sid.String() + ")")
		must(err)
		sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
		path := filepath.Join(root, "private")
		must(windows.CreateDirectory(ptr(path), sa))
		f := filepath.Join(path, "value.json")
		must(os.WriteFile(f, []byte("{}"), 0600))
		actual, err := windows.GetNamedSecurityInfo(f, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		must(err)
		sddl := actual.String()
		return strings.Contains(sddl, u.User.Sid.String()) && !strings.Contains(sddl, ";;;WD)") && !strings.Contains(sddl, ";;;BU)") && !strings.Contains(sddl, ";;;AU)"), "protected parent DACL; child inherits owner/SYSTEM; no second-user access test"
	})
	check("directory_sync_is_not_unix", func() (bool, string) {
		f, err := os.Open(root)
		must(err)
		defer f.Close()
		err = f.Sync()
		return err != nil, "os.File.Sync on directory returned error: " + fmt.Sprint(err != nil)
	})
	check("replacement_share_delete", func() (bool, string) {
		old, next := filepath.Join(root, "old"), filepath.Join(root, "next")
		must(os.WriteFile(old, []byte("old"), 0600))
		must(os.WriteFile(next, []byte("next"), 0600))
		h, err := windows.CreateFile(ptr(old), windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
		must(err)
		defer func() {
			if h != 0 {
				windows.CloseHandle(h)
			}
		}()
		liveErr := windows.MoveFileEx(ptr(next), ptr(old), windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		must(windows.CloseHandle(h))
		h = 0
		if liveErr != nil {
			must(windows.MoveFileEx(ptr(next), ptr(old), windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH))
		}
		b, err := os.ReadFile(old)
		must(err)
		return string(b) == "next", fmt.Sprintf("same-volume MoveFileEx; live-reader error=%v; closed-reader retry succeeded; no power-loss claim", liveErr)
	})
	check("no_replace_publish", func() (bool, string) {
		a, b := filepath.Join(root, "publish-source"), filepath.Join(root, "publish-target")
		must(os.WriteFile(a, []byte("new"), 0600))
		must(os.WriteFile(b, []byte("old"), 0600))
		err := windows.MoveFileEx(ptr(a), ptr(b), windows.MOVEFILE_WRITE_THROUGH)
		body, e := os.ReadFile(b)
		must(e)
		return err != nil && string(body) == "old", "existing target preserved"
	})
	check("running_image_rename", func() (bool, string) {
		path, backup := filepath.Join(root, "installed.exe"), filepath.Join(root, "installed.old.exe")
		body, err := os.ReadFile(exe)
		must(err)
		must(os.WriteFile(path, body, 0700))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, "sleep")
		must(cmd.Start())
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		err = windows.MoveFileEx(ptr(path), ptr(backup), windows.MOVEFILE_WRITE_THROUGH)
		return err == nil, fmt.Sprintf("active .exe rename error=%v; replacement/update recovery still requires integration tests", err)
	})
	check("exe_argv_roundtrip", func() (bool, string) {
		args := []string{"", "two words", "한글", `a"b`, `C:\trailing\`, "%PATH%", "!bang!", "a&b", "a|b", "a^b", "(x)", "line\nfeed"}
		b, err := exec.Command(exe, append([]string{"args"}, args...)...).Output()
		must(err)
		var got []string
		must(json.Unmarshal(b, &got))
		return reflect.DeepEqual(args, got), "Go CRT-compatible .exe argv, including shell metacharacters"
	})
	check("batch_direct_observation", func() (bool, string) {
		path := filepath.Join(root, "fixture.cmd")
		must(os.WriteFile(path, []byte("@exit /b 0\r\n"), 0600))
		err := exec.Command(path).Run()
		return true, fmt.Sprintf("direct .cmd error=%v; observation only, no argv guarantee", err)
	})
	check("batch_conservative_argv", func() (bool, string) {
		path := filepath.Join(root, "argv fixture.cmd")
		must(os.WriteFile(path, []byte("@\""+exe+"\" args %*\r\n"), 0600))
		args := []string{"", "two words", "한글", `C:\trailing\`, "!bang!", "a&b", "a|b", "a^b", "(x)", "<xml>"}
		line := `cmd.exe /d /e:on /v:off /s /c ""` + path + `"`
		for _, arg := range args {
			if strings.ContainsAny(arg, "\x00\r\n\"%") {
				panic("outside conservative batch domain")
			}
			slashes := len(arg) - len(strings.TrimRight(arg, `\`))
			line += ` "` + arg + strings.Repeat(`\`, slashes) + `"`
		}
		line += `"`
		cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
		b, err := cmd.Output()
		must(err)
		var got []string
		must(json.Unmarshal(b, &got))
		return reflect.DeepEqual(args, got), "explicit cmd adapter; rejects percent, double quote, CR/LF/NUL; trusted batch may reinterpret its own args"
	})
	check("loopback_exclusive_bind", func() (bool, string) {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		must(err)
		defer l.Close()
		second, err := net.Listen("tcp4", l.Addr().String())
		if err == nil {
			second.Close()
		}
		return err != nil, "second bind rejected"
	})
	check("job_at_creation_kill_on_close", func() (bool, string) {
		job, err := windows.CreateJobObject(nil, nil)
		must(err)
		defer func() {
			if job != 0 {
				windows.CloseHandle(job)
			}
		}()
		limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
		must(err)
		attrs, err := windows.NewProcThreadAttributeList(1)
		must(err)
		defer attrs.Delete()
		const jobList = 0x0002000d // PROC_THREAD_ATTRIBUTE_JOB_LIST, Windows SDK.
		must(attrs.Update(jobList, unsafe.Pointer(&job), unsafe.Sizeof(job)))
		si := windows.StartupInfoEx{}
		si.Cb = uint32(unsafe.Sizeof(si))
		si.ProcThreadAttributeList = attrs.List()
		pi := windows.ProcessInformation{}
		must(windows.CreateProcess(ptr(exe), ptr(windows.EscapeArg(exe)+" sleep"), nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT, nil, nil, &si.StartupInfo, &pi))
		defer windows.CloseHandle(pi.Process)
		defer windows.CloseHandle(pi.Thread)
		defer windows.TerminateProcess(pi.Process, 1)
		state, err := windows.WaitForSingleObject(pi.Process, 0)
		must(err)
		if state != uint32(windows.WAIT_TIMEOUT) {
			return false, "child exited before job close"
		}
		must(windows.CloseHandle(job))
		job = 0
		state, err = windows.WaitForSingleObject(pi.Process, 5000)
		must(err)
		return state == windows.WAIT_OBJECT_0, "atomic job association at CreateProcess; close kills child; tree/fault matrix still required"
	})
	version := windows.RtlGetVersion()
	report := map[string]any{"schema_version": 1, "go": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "windows_build": version.BuildNumber, "elevated": windows.GetCurrentProcessToken().IsElevated(), "results": results}
	must(json.NewEncoder(os.Stdout).Encode(report))
	for _, r := range results {
		if !r.Pass {
			os.Exit(1)
		}
	}
}
