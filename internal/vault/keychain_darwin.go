package vault

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// KeychainProtector stores only this vault's root key as a generic password.
// Explicit interactive setup may allow the OS prompt; agent use defaults to
// refusing interaction. This is user key storage, not same-user app isolation.
type KeychainProtector struct {
	VaultID          string
	AllowInteraction bool
}

func (p KeychainProtector) Identity() ProtectionIdentity {
	return ProtectionIdentity{Backend: "keychain", VaultID: p.VaultID}
}

type keychainAPI struct {
	dictCreate     func(uintptr, int, uintptr, uintptr) uintptr
	dictSet        func(uintptr, uintptr, uintptr)
	stringCreate   func(uintptr, *byte, int, uint32, bool) uintptr
	dataCreate     func(uintptr, *byte, int) uintptr
	dataLength     func(uintptr) int
	dataBytes      func(uintptr) *byte
	dataType       func() uintptr
	getType        func(uintptr) uintptr
	release        func(uintptr)
	readPointer    func(*uintptr, uintptr, uintptr) uintptr
	add            func(uintptr, *uintptr) int32
	copy           func(uintptr, *uintptr) int32
	delete         func(uintptr) int32
	getInteraction func(*uint8) int32
	setInteraction func(uint8) int32
	constants      map[string]uintptr
}

// File-based macOS keychains ignore the SecItem authentication-UI query key.
// Their interaction setting is process-global: serialize our native calls and
// restore the previous setting even on error. Native session calls still run in
// a bounded helper process because an OS call cannot observe a Go cancellation.
var keychainInteractionMu sync.Mutex

var loadKeychain = sync.OnceValues(func() (*keychainAPI, error) {
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, ErrUnavailable
	}
	sec, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		purego.Dlclose(cf)
		return nil, ErrUnavailable
	}
	a := &keychainAPI{constants: make(map[string]uintptr)}
	bindings := []struct {
		h    uintptr
		name string
		fn   any
	}{
		{cf, "CFDictionaryCreateMutable", &a.dictCreate}, {cf, "CFDictionarySetValue", &a.dictSet},
		{cf, "CFStringCreateWithBytes", &a.stringCreate}, {cf, "CFDataCreate", &a.dataCreate},
		{cf, "CFDataGetLength", &a.dataLength}, {cf, "CFDataGetBytePtr", &a.dataBytes},
		{cf, "CFDataGetTypeID", &a.dataType}, {cf, "CFGetTypeID", &a.getType}, {cf, "CFRelease", &a.release},
		{cf, "memcpy", &a.readPointer},
		{sec, "SecItemAdd", &a.add}, {sec, "SecItemCopyMatching", &a.copy}, {sec, "SecItemDelete", &a.delete},
		{sec, "SecKeychainGetUserInteractionAllowed", &a.getInteraction},
		{sec, "SecKeychainSetUserInteractionAllowed", &a.setInteraction},
	}
	for _, b := range bindings {
		s, err := purego.Dlsym(b.h, b.name)
		if err != nil {
			purego.Dlclose(sec)
			purego.Dlclose(cf)
			return nil, ErrUnavailable
		}
		purego.RegisterFunc(b.fn, s)
	}
	for _, name := range []string{"kSecClass", "kSecClassGenericPassword", "kSecAttrService", "kSecAttrAccount", "kSecUseAuthenticationUI", "kSecUseAuthenticationUIFail", "kSecValueData", "kSecReturnData", "kSecMatchLimit", "kSecMatchLimitOne"} {
		s, err := purego.Dlsym(sec, name)
		if err != nil {
			purego.Dlclose(sec)
			purego.Dlclose(cf)
			return nil, ErrUnavailable
		}
		// Exported CF constants are pointer-sized data symbols, not handles.
		// Read through the native ABI without converting a Go uintptr back to
		// unsafe.Pointer (which would violate Go's pointer provenance rules).
		var ref uintptr
		a.readPointer(&ref, s, unsafe.Sizeof(ref))
		a.constants[name] = ref
	}
	s, err := purego.Dlsym(cf, "kCFBooleanTrue")
	if err != nil {
		purego.Dlclose(sec)
		purego.Dlclose(cf)
		return nil, ErrUnavailable
	}
	var trueRef uintptr
	a.readPointer(&trueRef, s, unsafe.Sizeof(trueRef))
	a.constants["true"] = trueRef
	// Successful handles remain loaded for the process lifetime: function and
	// constant pointers are shared by subsequent operations.
	return a, nil
})

// query uses a non-retaining dictionary. All dynamically created CF objects are
// kept alive until after the Security call and released together by cleanup.
func (p KeychainProtector) query() (*keychainAPI, uintptr, func(), error) {
	if !validID(p.VaultID) {
		return nil, 0, nil, ErrInvalid
	}
	a, err := loadKeychain()
	if err != nil {
		return nil, 0, nil, err
	}
	keychainInteractionMu.Lock()
	var previous uint8
	if err := keychainStatus(a.getInteraction(&previous)); err != nil {
		keychainInteractionMu.Unlock()
		return nil, 0, nil, err
	}
	allow := uint8(0)
	if p.AllowInteraction {
		allow = 1
	}
	if err := keychainStatus(a.setInteraction(allow)); err != nil {
		keychainInteractionMu.Unlock()
		return nil, 0, nil, err
	}
	restore := func() {
		a.setInteraction(previous)
		keychainInteractionMu.Unlock()
	}
	d := a.dictCreate(0, 0, 0, 0)
	if d == 0 {
		restore()
		return nil, 0, nil, ErrUnavailable
	}
	owned := []uintptr{d}
	cleanup := func() {
		// Release the dictionary before its non-retained values.
		for _, ref := range owned {
			a.release(ref)
		}
		restore()
	}
	for _, item := range []struct{ key, value string }{{"kSecAttrService", "devtools.vault.v1"}, {"kSecAttrAccount", p.VaultID}} {
		b := []byte(item.value)
		v := a.stringCreate(0, &b[0], len(b), 0x08000100, false) // kCFStringEncodingUTF8
		runtime.KeepAlive(b)
		if v == 0 {
			cleanup()
			return nil, 0, nil, ErrUnavailable
		}
		owned = append(owned, v)
		a.dictSet(d, a.constants[item.key], v)
	}
	a.dictSet(d, a.constants["kSecClass"], a.constants["kSecClassGenericPassword"])
	if !p.AllowInteraction {
		a.dictSet(d, a.constants["kSecUseAuthenticationUI"], a.constants["kSecUseAuthenticationUIFail"])
	}
	return a, d, cleanup, nil
}

func keychainStatus(code int32) error {
	switch code {
	case 0:
		return nil
	case -25300:
		return ErrKeyMissing
	case -25308, -25315:
		return ErrInteractionRequired
	case -128, -25293:
		return ErrDenied
	case -25299:
		return ErrRecovery // never replace an existing root key
	default:
		return ErrUnavailable
	}
}

func (p KeychainProtector) Wrap(ctx context.Context, key []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := osKeyPayload("keychain", p.VaultID, key)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	a, q, cleanup, err := p.query()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	data := a.dataCreate(0, &b[0], len(b))
	runtime.KeepAlive(b)
	if data == 0 {
		return nil, ErrUnavailable
	}
	defer a.release(data)
	a.dictSet(q, a.constants["kSecValueData"], data)
	if err := keychainStatus(a.add(q, nil)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte("keychain/v1/" + p.VaultID), nil
}

func (p KeychainProtector) Unwrap(ctx context.Context, wrapped []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !bytes.Equal(wrapped, []byte("keychain/v1/"+p.VaultID)) {
		return nil, ErrInvalid
	}
	a, q, cleanup, err := p.query()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	a.dictSet(q, a.constants["kSecReturnData"], a.constants["true"])
	a.dictSet(q, a.constants["kSecMatchLimit"], a.constants["kSecMatchLimitOne"])
	var result uintptr
	code := a.copy(q, &result)
	if result != 0 {
		defer a.release(result)
	}
	if err := keychainStatus(code); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result == 0 || a.getType(result) != a.dataType() {
		return nil, ErrIntegrity
	}
	n := a.dataLength(result)
	ptr := a.dataBytes(result)
	if n <= 0 || n > MaxKeySlotBytes || ptr == nil {
		return nil, ErrIntegrity
	}
	return osKeyFromPayload("keychain", p.VaultID, unsafe.Slice(ptr, n))
}

// Remove deletes exactly this vault item during explicit recovery or disposable
// native tests. Callers must never invoke it as an automatic unwrap fallback.
func (p KeychainProtector) Remove(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a, q, cleanup, err := p.query()
	if err != nil {
		return err
	}
	defer cleanup()
	return keychainStatus(a.delete(q))
}
