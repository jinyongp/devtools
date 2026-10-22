package projectfile

import (
	"errors"
	"github.com/ebitengine/purego"
	"os"
	"sync"
	"unsafe"
)

// Darwin's extended ACL is separate from listxattr. Use descriptor APIs so a
// pathname replacement cannot redirect the permissions being copied.
// https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man3/acl_get.3.html
const aclExtended = 0x100

var aclAPI struct {
	sync.Once
	err  error
	get  func(int32, uint32) uintptr
	set  func(int32, uintptr, uint32) int32
	text func(uintptr, *int64) uintptr
	free func(uintptr) int32
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
	}{{"acl_get_fd_np", &aclAPI.get}, {"acl_set_fd_np", &aclAPI.set}, {"acl_to_text", &aclAPI.text}, {"acl_free", &aclAPI.free}} {
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
	acl := aclAPI.get(int32(f.Fd()), aclExtended)
	if acl == 0 {
		return 0, errors.New("extended ACL unavailable")
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
	p := aclAPI.text(acl, &n)
	if p == 0 || n < 0 || n > 1<<20 {
		return "", errors.New("ACL text unavailable")
	}
	defer aclAPI.free(p)
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n))), nil
}
func copyExtendedACL(src, dst *os.File) error {
	acl, err := getACL(src)
	if err != nil {
		return err
	}
	defer aclAPI.free(acl)
	if aclAPI.set(int32(dst.Fd()), acl, aclExtended) != 0 {
		return errors.New("extended ACL copy failed")
	}
	return nil
}
