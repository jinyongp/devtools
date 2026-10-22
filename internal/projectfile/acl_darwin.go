package projectfile

import (
	"errors"
	"github.com/ebitengine/purego"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// Darwin's extended ACL is separate from listxattr. Use descriptor APIs so a
// pathname replacement cannot redirect the permissions being copied.
// https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man3/acl_get.3.html
const aclExtended = 0x100

var aclAPI struct {
	sync.Once
	err  error
	get  uintptr
	init uintptr
	set  uintptr
	text uintptr
	free func(uintptr) int32
	copy func(*byte, uintptr, uintptr) uintptr
}

func loadACL() {
	h, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		aclAPI.err = err
		return
	}
	var free, copy uintptr
	for _, symbol := range []struct {
		name    string
		address *uintptr
	}{{"acl_get_fd_np", &aclAPI.get}, {"acl_init", &aclAPI.init}, {"acl_set_fd_np", &aclAPI.set}, {"acl_to_text", &aclAPI.text}, {"acl_free", &free}, {"memcpy", &copy}} {
		address, err := purego.Dlsym(h, symbol.name)
		if err != nil {
			aclAPI.err = err
			return
		}
		*symbol.address = address
	}
	purego.RegisterFunc(&aclAPI.free, free)
	purego.RegisterFunc(&aclAPI.copy, copy)
}
func getACL(f *os.File) (uintptr, error) {
	aclAPI.Do(loadACL)
	if aclAPI.err != nil {
		return 0, aclAPI.err
	}
	// Capture errno inside the native call: even a locked Go thread can run
	// runtime code that changes errno between separate foreign calls.
	// An absent FILESEC_ACL property is ENOENT even for an existing descriptor;
	// normalize only that case to an empty ACL. All other retrieval failures
	// must still prevent publication.
	// https://github.com/apple-oss-distributions/Libc/blob/main/gen/filesec.c
	acl, _, errno := purego.SyscallN(aclAPI.get, f.Fd(), aclExtended)
	operation := "acl_get_fd_np"
	if acl == 0 && syscall.Errno(errno) == syscall.ENOENT {
		operation = "acl_init"
		acl, _, errno = purego.SyscallN(aclAPI.init, 0)
	}
	if acl == 0 {
		return 0, os.NewSyscallError(operation, syscall.Errno(errno))
	}
	return acl, nil
}
func extendedACL(f *os.File) (string, error) {
	acl, err := getACL(f)
	if err != nil {
		return "", err
	}
	defer aclAPI.free(acl)
	var n int64
	p, _, errno := purego.SyscallN(aclAPI.text, acl, uintptr(unsafe.Pointer(&n)))
	if p == 0 {
		return "", os.NewSyscallError("acl_to_text", syscall.Errno(errno))
	}
	defer aclAPI.free(p)
	if n < 0 || n > 1<<20 {
		return "", os.NewSyscallError("acl_to_text", errors.New("invalid ACL text length"))
	}
	text := make([]byte, int(n))
	if len(text) != 0 {
		// Copy native memory without converting a returned uintptr to a Go pointer.
		aclAPI.copy(&text[0], p, uintptr(n))
	}
	return string(text), nil
}
func copyExtendedACL(src, dst *os.File) error {
	acl, err := getACL(src)
	if err != nil {
		return err
	}
	defer aclAPI.free(acl)
	result, _, errno := purego.SyscallN(aclAPI.set, dst.Fd(), acl, aclExtended)
	if int32(result) != 0 {
		return os.NewSyscallError("acl_set_fd_np", syscall.Errno(errno))
	}
	return nil
}
