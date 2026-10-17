package services

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/jinyongp/devtools/internal/process"
	"golang.org/x/sys/windows"
)

func startSupervisor(cmd *exec.Cmd, lease *os.File) error {
	if lease == nil || len(cmd.Args) != 5 {
		return errors.New("invalid supervisor handoff")
	}
	handle := windows.Handle(lease.Fd())
	if int64(handle) <= 0 {
		return errors.New("invalid supervisor lease handle")
	}
	kind, err := windows.GetFileType(handle)
	if err != nil || kind != windows.FILE_TYPE_DISK {
		return errors.New("invalid supervisor lease file")
	}
	var child windows.Handle
	current := windows.CurrentProcess()
	if err := windows.DuplicateHandle(current, handle, current, &child, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return err
	}
	defer windows.CloseHandle(child)
	cmd.Args[4] = strconv.FormatUint(uint64(child), 10)
	cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(child)}}
	return process.StartDetached(cmd)
}

func OpenInheritedLease(argument string) (*os.File, error) {
	number, err := strconv.ParseUint(argument, 10, 64)
	if err != nil || number == 0 || int64(number) < 0 || uint64(uintptr(number)) != number {
		return nil, errors.New("invalid inherited lease handle")
	}
	handle := windows.Handle(number)
	kind, err := windows.GetFileType(handle)
	if err != nil || kind != windows.FILE_TYPE_DISK {
		return nil, errors.New("invalid inherited lease file")
	}
	// Adoption checks the exact private file identity and exclusive data access.
	return os.NewFile(uintptr(handle), "process-lease"), nil
}
