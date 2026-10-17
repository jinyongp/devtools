//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

func StartDetached(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return command.Start()
}
