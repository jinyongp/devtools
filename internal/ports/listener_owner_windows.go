package ports

import (
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getExtendedTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func listenerProcesses(ctx context.Context, port int) ([]ListenerProcess, error) {
	var result []ListenerProcess
	for _, family := range []struct{ address, rowBytes, portOffset, pidOffset int }{{2, 24, 8, 20}, {23, 56, 20, 52}} {
		var size uint32
		var buffer []byte
		complete := false
		for attempt := 0; attempt < 3; attempt++ {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			var pointer uintptr
			if len(buffer) > 0 {
				pointer = uintptr(unsafe.Pointer(&buffer[0]))
			}
			// TCP_TABLE_OWNER_PID_LISTENER = 3, for both IPv4 and IPv6.
			code, _, _ := getExtendedTCPTable.Call(pointer, uintptr(unsafe.Pointer(&size)), 0, uintptr(family.address), 3, 0)
			if code == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
				if size < 4 || size > 16<<20 {
					return result, fmt.Errorf("invalid TCP table size")
				}
				buffer = make([]byte, size)
				continue
			}
			if code != 0 {
				return result, syscall.Errno(code)
			}
			if len(buffer) < 4 {
				return result, fmt.Errorf("incomplete TCP table")
			}
			count := int(binary.LittleEndian.Uint32(buffer))
			if count > (len(buffer)-4)/family.rowBytes {
				return result, fmt.Errorf("invalid TCP table row count")
			}
			for row := 0; row < count; row++ {
				data := buffer[4+row*family.rowBytes:]
				if int(binary.BigEndian.Uint16(data[family.portOffset:])) != port {
					continue
				}
				pid := int(binary.LittleEndian.Uint32(data[family.pidOffset:]))
				handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
				if err != nil {
					return result, err
				}
				var name [32768]uint16
				length := uint32(len(name))
				err = windows.QueryFullProcessImageName(handle, 0, &name[0], &length)
				windows.CloseHandle(handle)
				if err != nil {
					return result, err
				}
				if _, err := runProcessIdentity(pid); err == nil {
					result = append(result, ListenerProcess{PID: pid, Program: filepath.Base(windows.UTF16ToString(name[:length]))})
				}
			}
			complete = true
			break
		}
		if !complete {
			return result, fmt.Errorf("TCP table changed during observation")
		}
	}
	return result, ctx.Err()
}
