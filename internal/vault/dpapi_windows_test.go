package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNativeDPAPIUserRoundtrip(t *testing.T) {
	p := DPAPIProtector{VaultID: "0123456789abcdef0123456789abcdef"}
	key := bytes.Repeat([]byte{0x47}, 32)
	wrapped, err := p.Wrap(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrapped, key) {
		t.Fatal("plaintext key persisted")
	}
	got, err := p.Unwrap(context.Background(), wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("roundtrip", err)
	}
	clear(got)
	other := DPAPIProtector{VaultID: "1123456789abcdef0123456789abcdef"}
	if _, err := other.Unwrap(context.Background(), wrapped); err == nil {
		t.Fatal("cross-vault unwrap")
	}
	for _, i := range []int{0, len(wrapped) / 2, len(wrapped) - 1} {
		bad := bytes.Clone(wrapped)
		bad[i] ^= 1
		if _, err := p.Unwrap(context.Background(), bad); err == nil {
			t.Fatal("corruption accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Unwrap(ctx, wrapped); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// ACL denial alone cannot establish DPAPI's identity binding: the other user
// receives the already-wrapped bytes in memory and must still fail to decrypt.
func TestNativeDPAPIRejectsDifferentWindowsIdentity(t *testing.T) {
	user, password := os.Getenv("DEVTOOLS_TEST_USER"), os.Getenv("DEVTOOLS_TEST_PASSWORD")
	if user == "" || password == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("disposable standard-account fixture required in CI")
		}
		t.Skip("requires disposable standard-account fixture")
	}
	ctx := context.Background()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	p := DPAPIProtector{VaultID: id}
	key := bytes.Repeat([]byte{0x71}, 32)
	wrapped, err := p.Wrap(ctx, key)
	if err != nil {
		t.Fatal("owner protection failed", err)
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
	defer token.Close()
	other, err := token.GetTokenUser()
	if err != nil || owner.User.Sid.Equals(other.User.Sid) {
		t.Fatal("fixture did not select a different SID")
	}
	var impersonation windows.Token
	if err = windows.DuplicateTokenEx(token, windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE, nil, windows.SecurityImpersonation, windows.TokenImpersonation, &impersonation); err != nil {
		t.Fatal("cannot prepare impersonation")
	}
	defer impersonation.Close()
	func() {
		runtime.LockOSThread()
		if err := windows.SetThreadToken(nil, impersonation); err != nil {
			runtime.UnlockOSThread()
			t.Fatal("cannot select alternate thread identity")
		}
		defer func() {
			if err := windows.RevertToSelf(); err != nil {
				t.Fatal("cannot restore original thread identity")
			}
			runtime.UnlockOSThread()
		}()
		var actual windows.Token
		if err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &actual); err != nil {
			t.Fatal("alternate identity was not applied")
		}
		observed, e := actual.GetTokenUser()
		actual.Close()
		if e != nil || !observed.User.Sid.Equals(other.User.Sid) {
			t.Fatal("alternate identity mismatch")
		}
		got, err := p.Unwrap(ctx, wrapped)
		leaked := len(got) > 0
		clear(got)
		if leaked || !errors.Is(err, ErrIntegrity) && !errors.Is(err, ErrDenied) {
			t.Fatal("DPAPI did not reject the alternate identity", err)
		}
	}()
	got, err := p.Unwrap(ctx, wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("original identity could not reopen the preserved key", err)
	}
	clear(got)
}
