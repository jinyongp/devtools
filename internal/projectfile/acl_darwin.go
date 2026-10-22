package projectfile

import (
	"errors"
	"github.com/ebitengine/purego"
	"os"
	"runtime"
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
	err   error
	get   func(int32, uint32) uintptr
	init  func(int32) uintptr
	errno func() *int32
	set   func(int32, uintptr, uint32) int32
	text  func(uintptr, *int64) *byte
	free  func(uintptr) int32
}

func loadACL() {
	h, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		aclAPI.err = err
		return
	}
	for _, symbol := range []struct {
		name string
		fn   any
	}{{"acl_get_fd_np", &aclAPI.get}, {"acl_init", &aclAPI.init}, {"__error", &aclAPI.errno}, {"acl_set_fd_np", &aclAPI.set}, {"acl_to_text", &aclAPI.text}, {"acl_free", &aclAPI.free}} {
		address, err := purego.Dlsym(h, symbol.name)
		if err != nil {
			aclAPI.err = err
			return
		}
		purego.RegisterFunc(symbol.fn, address)
	}
}
func getACL(f *os.File) (uintptr, error) {
	aclAPI.Do(loadACL)
	if aclAPI.err != nil {
		return 0, aclAPI.err
	}
	// errno belongs to the native thread. An absent FILESEC_ACL property is
	// ENOENT even for an existing descriptor; normalize only that case to an
	// empty ACL. All other retrieval failures must still prevent publication.
	// https://github.com/apple-oss-distributions/Libc/blob/main/gen/filesec.c
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	acl := aclAPI.get(int32(f.Fd()), aclExtended)
	operation := "acl_get_fd_np"
	if acl == 0 && syscall.Errno(*aclAPI.errno()) == syscall.ENOENT {
		operation = "acl_init"
		acl = aclAPI.init(0)
	}
	if acl == 0 {
		return 0, os.NewSyscallError(operation, syscall.Errno(*aclAPI.errno()))
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
	runtime.LockOSThread()
	p := aclAPI.text(acl, &n)
	errno := syscall.Errno(*aclAPI.errno())
	runtime.UnlockOSThread()
	if p == nil {
		return "", os.NewSyscallError("acl_to_text", errno)
	}
	defer aclAPI.free(uintptr(unsafe.Pointer(p)))
	if n < 0 || n > 1<<20 {
		return "", os.NewSyscallError("acl_to_text", errors.New("invalid ACL text length"))
	}
	return string(unsafe.Slice(p, int(n))), nil
}
func copyExtendedACL(src, dst *os.File) error {
	acl, err := getACL(src)
	if err != nil {
		return err
	}
	defer aclAPI.free(acl)
	runtime.LockOSThread()
	result := aclAPI.set(int32(dst.Fd()), acl, aclExtended)
	errno := syscall.Errno(*aclAPI.errno())
	runtime.UnlockOSThread()
	if result != 0 {
		return os.NewSyscallError("acl_set_fd_np", errno)
	}
	return nil
}
