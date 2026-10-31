package ports

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func runProcessIdentity(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", err
	}
	if exited.HighDateTime != 0 || exited.LowDateTime != 0 {
		return "", fmt.Errorf("process exited")
	}
	return fmt.Sprintf("%d:%d", created.HighDateTime, created.LowDateTime), nil
}
