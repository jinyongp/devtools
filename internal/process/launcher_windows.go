package process

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const procThreadAttributeJobList = 0x0002000d // Windows SDK PROC_THREAD_ATTRIBUTE_JOB_LIST.
const nativeStopGrace = 3 * time.Second

type nativeChild struct {
	process          windows.Handle
	job              windows.Handle
	pid              uint32
	streams          *nativeStreams
	done             chan struct{}
	code             uint32
	waitErr          error
	closeJobOnce     sync.Once
	closeJobErr      error
	closeProcessOnce sync.Once
	closeProcessErr  error
}

func newNativeJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}
func nativeEnvironment(env []string) ([]uint16, error) {
	if env == nil {
		env = os.Environ()
	}
	for _, value := range env {
		if strings.ContainsRune(value, '\x00') {
			return nil, os.ErrInvalid
		}
	}
	hasSystemRoot := false
	for _, value := range env {
		key, _, ok := splitEnvironment(value)
		if ok && strings.EqualFold(key, "SYSTEMROOT") {
			hasSystemRoot = true
			break
		}
	}
	// Match os/exec's critical Windows environment behavior, including an
	// explicitly empty SYSTEMROOT override instead of silently repairing it.
	if !hasSystemRoot {
		env = append(append([]string{}, env...), "SYSTEMROOT="+os.Getenv("SYSTEMROOT"))
	}
	env = Environment(env, nil)
	return utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00")), nil
}
func createNativeProcess(command *exec.Cmd, job windows.Handle, streams *nativeStreams, flags uint32) (windows.ProcessInformation, error) {
	var result windows.ProcessInformation
	if job == 0 {
		return result, os.ErrInvalid
	}
	application, err := windows.UTF16PtrFromString(command.Path)
	if err != nil {
		return result, err
	}
	line := ""
	if command.SysProcAttr != nil {
		line = command.SysProcAttr.CmdLine
	}
	if line == "" {
		args := make([]string, len(command.Args))
		for i, arg := range command.Args {
			args[i] = syscall.EscapeArg(arg)
		}
		line = strings.Join(args, " ")
	}
	cmdline, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return result, err
	}
	var directory *uint16
	if command.Dir != "" {
		directory, err = windows.UTF16PtrFromString(command.Dir)
		if err != nil {
			return result, err
		}
	}
	environment, err := nativeEnvironment(command.Env)
	if err != nil {
		return result, err
	}
	current := windows.CurrentProcess()
	handles := make([]windows.Handle, 0, 3)
	defer func() {
		for _, handle := range handles {
			_ = windows.CloseHandle(handle)
		}
	}()
	for _, file := range streams.files {
		source := windows.Handle(file.Fd())
		if source == 0 || int64(source) < 0 {
			return result, os.ErrInvalid
		}
		if kind, err := windows.GetFileType(source); err != nil || kind != windows.FILE_TYPE_DISK && kind != windows.FILE_TYPE_PIPE && kind != windows.FILE_TYPE_CHAR {
			return result, os.ErrInvalid
		}
		var copy windows.Handle
		if err = windows.DuplicateHandle(current, source, current, &copy, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return result, err
		}
		handles = append(handles, copy)
	}
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return result, err
	}
	defer attributes.Delete()
	if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return result, err
	}
	if err = attributes.Update(procThreadAttributeJobList, unsafe.Pointer(&job), unsafe.Sizeof(job)); err != nil {
		return result, err
	}
	start := windows.StartupInfoEx{}
	start.Cb = uint32(unsafe.Sizeof(start))
	start.Flags = windows.STARTF_USESTDHANDLES
	start.StdInput, start.StdOutput, start.StdErr = handles[0], handles[1], handles[2]
	start.ProcThreadAttributeList = attributes.List()
	if command.SysProcAttr != nil && command.SysProcAttr.HideWindow {
		start.Flags |= windows.STARTF_USESHOWWINDOW
		start.ShowWindow = windows.SW_HIDE
	}
	err = windows.CreateProcess(application, cmdline, nil, nil, true, flags|windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT, &environment[0], directory, &start.StartupInfo, &result)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(environment)
	runtime.KeepAlive(job)
	return result, err
}
func startNativeOwned(ctx context.Context, command *exec.Cmd) (child *nativeChild, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	streams, err := prepareNativeStreams(command.Stdin, command.Stdout, command.Stderr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			streams.closeAll()
		}
	}()
	job, err := newNativeJob()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = windows.CloseHandle(job)
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	info, err := createNativeProcess(command, job, streams, windows.CREATE_NEW_PROCESS_GROUP)
	if err != nil {
		return nil, err
	}
	_ = windows.CloseHandle(info.Thread)
	child = &nativeChild{process: info.Process, job: job, pid: info.ProcessId, streams: streams, done: make(chan struct{})}
	streams.start()
	go func() {
		defer close(child.done)
		result, e := windows.WaitForSingleObject(child.process, windows.INFINITE)
		if e != nil {
			child.waitErr = e
			return
		}
		if result != windows.WAIT_OBJECT_0 {
			child.waitErr = errors.New("unexpected process wait result")
			return
		}
		child.waitErr = windows.GetExitCodeProcess(child.process, &child.code)
	}()
	return child, nil
}
func (c *nativeChild) closeJob() error {
	c.closeJobOnce.Do(func() { c.closeJobErr = windows.CloseHandle(c.job) })
	return c.closeJobErr
}

// The SDK accounting layout is four LARGE_INTEGER times followed by four DWORD
// counts, identically aligned on supported Windows amd64 and arm64.
type nativeJobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodUserTime, ThisPeriodKernelTime       int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses uint32
}

func (c *nativeChild) activeProcesses() (uint32, error) {
	var info nativeJobAccounting
	err := windows.QueryInformationJobObject(c.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	return info.ActiveProcesses, err
}
func (c *nativeChild) stop() (int, error) {
	// CTRL_BREAK can target this live child group only. A pipe-only host may have
	// no attached console; grace still expires before owned-tree termination.
	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, c.pid)
	timer := time.NewTimer(nativeStopGrace)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		active, err := c.activeProcesses()
		if err != nil {
			_ = c.closeJob()
			<-c.done
			return 0, err
		}
		if active == 0 {
			<-c.done
			return 130, c.waitErr
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			err = windows.TerminateJobObject(c.job, 137)
			closeErr := c.closeJob()
			<-c.done
			if err != nil {
				return 0, errors.Join(err, closeErr, c.waitErr)
			}
			return 137, errors.Join(closeErr, c.waitErr)
		}
	}
}
func (c *nativeChild) wait(ctx context.Context) (int, error) {
	defer c.closeProcess()
	var code int
	var err error
	select {
	case <-ctx.Done():
		code, err = c.stop()
	case <-c.done:
		if ctx.Err() != nil {
			code, err = c.stop()
		} else {
			code, err = int(c.code), c.waitErr
		}
	}
	// Closing the final non-inherited job handle cleans descendants even after
	// the direct child exits normally. Drain their pipes only after that close.
	err = errors.Join(err, c.closeJob())
	streamErr := c.streams.finish()
	if err == nil && code == 0 {
		err = streamErr
	}
	return code, err
}

func (c *nativeChild) closeProcess() error {
	c.closeProcessOnce.Do(func() { c.closeProcessErr = windows.CloseHandle(c.process) })
	return c.closeProcessErr
}
func (c *nativeChild) cleanup() {
	_ = c.closeJob()
	<-c.done
	_ = c.streams.finish()
	_ = c.closeProcess()
}
