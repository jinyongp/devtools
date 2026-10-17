package process

import (
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Documented Process Snapshotting ABI for amd64/arm64 PSS_HANDLE_ENTRY.
// No VA clone, remote process or production dependency is used.
// Object names are never read or recorded.
type snapshotHandleEntry struct {
	Handle                                                                                    windows.Handle
	Flags, ObjectType                                                                         uint32
	CaptureTime                                                                               windows.Filetime
	Attributes, GrantedAccess, HandleCount, PointerCount, PagedPoolCharge, NonPagedPoolCharge uint32
	CreationTime                                                                              windows.Filetime
	TypeNameLength                                                                            uint16
	TypeName                                                                                  *uint16
	ObjectNameLength                                                                          uint16
	ObjectName                                                                                *uint16
	Specific                                                                                  [6]uintptr
}

func ownedHandleTypes(t *testing.T, job windows.Handle, file *os.File) map[string]int {
	t.Helper()
	dll := windows.NewLazySystemDLL("kernel32.dll")
	capture := dll.NewProc("PssCaptureSnapshot")
	free := dll.NewProc("PssFreeSnapshot")
	createMarker := dll.NewProc("PssWalkMarkerCreate")
	freeMarker := dll.NewProc("PssWalkMarkerFree")
	walk := dll.NewProc("PssWalkSnapshot")
	var snapshot, marker windows.Handle
	code, _, _ := capture.Call(uintptr(windows.CurrentProcess()), 0x4, 0, uintptr(unsafe.Pointer(&snapshot)))
	if code != 0 {
		t.Fatal("PssCaptureSnapshot", windows.Errno(code))
	}
	defer free.Call(uintptr(windows.CurrentProcess()), uintptr(snapshot))
	code, _, _ = createMarker.Call(0, uintptr(unsafe.Pointer(&marker)))
	if code != 0 {
		t.Fatal("PssWalkMarkerCreate", windows.Errno(code))
	}
	defer freeMarker.Call(uintptr(marker))
	counts := map[string]int{}
	seenJob, seenFile := false, false
	for {
		var entry snapshotHandleEntry
		code, _, _ = walk.Call(uintptr(snapshot), 2, uintptr(marker), uintptr(unsafe.Pointer(&entry)), unsafe.Sizeof(entry))
		if code == uintptr(windows.ERROR_NO_MORE_ITEMS) {
			break
		}
		if code != 0 {
			t.Fatal("PssWalkSnapshot", windows.Errno(code))
		}
		// PSS need not provide names for every object type. Query only its type,
		// never ObjectName; a concurrently closed runtime handle is not a leak.
		kind, exists := snapshotObjectType(t, entry.Handle)
		if !exists {
			continue
		}

		counts[kind]++
		if entry.Handle == job {
			if kind != "Job" {
				t.Fatalf("reference Job type=%q", kind)
			}
			seenJob = true
		}
		if entry.Handle == windows.Handle(file.Fd()) {
			if kind != "File" {
				t.Fatalf("reference File type=%q", kind)
			}
			seenFile = true
		}
	}
	if !seenJob || !seenFile {
		t.Fatal("handle snapshot omitted reference resources")
	}
	return counts
}

func snapshotObjectType(t *testing.T, handle windows.Handle) (string, bool) {
	t.Helper()
	query := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQueryObject")
	buffer := make([]byte, 4096)
	var required uint32
	status, _, _ := query.Call(uintptr(handle), 2, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&required)))
	if uint32(status) == 0xc0000008 {
		return "", false
	} // snapshot handle closed in the meantime
	if status != 0 {
		t.Fatal("NtQueryObject(type)", windows.NTStatus(status))
	}
	name := (*windows.NTUnicodeString)(unsafe.Pointer(&buffer[0]))
	if name.Length == 0 || name.Length%2 != 0 || name.Length > 2048 || name.Buffer == nil {
		t.Fatal("invalid snapshot object type")
	}
	kind := name.String()
	runtime.KeepAlive(buffer)
	return kind, true
}
