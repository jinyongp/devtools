package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A new hidden console separates the supervisor from the invoking terminal.
// It remains subject to any enclosing host Job; no breakaway is requested.
func StartDetached(command *exec.Cmd) error {
	if len(command.ExtraFiles) != 0 {
		return errors.New("Windows ExtraFiles require an explicit handle handoff")
	}
	attr := syscall.SysProcAttr{}
	if command.SysProcAttr != nil {
		attr = *command.SysProcAttr
	}
	if attr.ParentProcess != 0 || attr.NoInheritHandles || attr.CreationFlags&(windows.DETACHED_PROCESS|windows.CREATE_BREAKAWAY_FROM_JOB) != 0 {
		return errors.New("incompatible detached supervisor attributes")
	}
	attr.CreationFlags |= windows.CREATE_NEW_CONSOLE
	attr.HideWindow = true
	command.SysProcAttr = &attr
	return command.Start()
}

var getConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// Fail closed when an internal supervisor is invoked in a shared console.
func RequireDedicatedConsole() error {
	var pid uint32
	count, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pid)), 1)
	if count != 1 || pid != uint32(os.Getpid()) {
		return errors.New("dedicated console required")
	}
	return nil
}
