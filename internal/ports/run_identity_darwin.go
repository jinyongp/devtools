package ports

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func runProcessIdentity(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if info.Proc.P_pid != int32(pid) {
		return "", fmt.Errorf("process identity changed")
	}
	return fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec), nil
}
