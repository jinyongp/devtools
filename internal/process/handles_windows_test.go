package process

import (
	"os"
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
	code, _, _ := capture.Call(uintptr(windows.CurrentProcess()), 0x4|0x8|0x10, 0, uintptr(unsafe.Pointer(&snapshot)))
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
		if entry.TypeNameLength == 0 || entry.TypeName == nil {
			t.Fatal("snapshot did not expose handle type name")
		}
		if entry.TypeNameLength%2 != 0 || entry.TypeNameLength > 2048 {
			t.Fatal("invalid snapshot type name")
		}
		kind := windows.UTF16ToString(unsafe.Slice(entry.TypeName, int(entry.TypeNameLength/2)))
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
