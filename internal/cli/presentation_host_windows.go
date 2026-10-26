package cli

import "golang.org/x/sys/windows"

func outputANSI(fd uintptr, _ string) bool {
	var mode uint32
	// Read the existing output mode. Styling must not change console state.
	return windows.GetConsoleMode(windows.Handle(fd), &mode) == nil && mode&0x5 == 0x5
}
