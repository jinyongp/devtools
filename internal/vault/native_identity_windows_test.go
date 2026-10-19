package vault

import (
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The native Windows workflow supplies its own temporary standard account.
// Never put its password in a child argv or failure message.
func nativeOtherWindowsUserToken(t *testing.T) windows.Token {
	t.Helper()
	user, password := os.Getenv("DEVTOOLS_TEST_USER"), os.Getenv("DEVTOOLS_TEST_PASSWORD")
	if user == "" || password == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("disposable standard-account fixture required in CI")
		}
		t.Skip("requires disposable standard-account fixture")
	}
	owner, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal("cannot resolve owner identity")
	}
	userPtr, _ := windows.UTF16PtrFromString(user)
	domainPtr, _ := windows.UTF16PtrFromString(".")
	passwordPtr, _ := windows.UTF16PtrFromString(password)
	var token windows.Token
	ok, _, _ := windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW").Call(uintptr(unsafe.Pointer(userPtr)), uintptr(unsafe.Pointer(domainPtr)), uintptr(unsafe.Pointer(passwordPtr)), 2, 0, uintptr(unsafe.Pointer(&token)))
	runtime.KeepAlive(passwordPtr)
	if ok == 0 {
		t.Fatal("cannot create disposable standard-account token")
	}
	t.Cleanup(func() { token.Close() })
	other, err := token.GetTokenUser()
	if err != nil || owner.User.Sid.Equals(other.User.Sid) {
		t.Fatal("fixture did not select a different SID")
	}
	return token
}
