//go:build !windows

package services

import (
	"errors"
	"github.com/jinyongp/devtools/internal/process"
	"os"
	"os/exec"
)

func startSupervisor(cmd *exec.Cmd, lease *os.File) error {
	cmd.ExtraFiles = []*os.File{lease}
	return process.StartDetached(cmd)
}

func OpenInheritedLease(argument string) (*os.File, error) {
	if argument != "3" {
		return nil, errors.New("invalid inherited lease descriptor")
	}
	return os.NewFile(3, "process-lease"), nil
}
