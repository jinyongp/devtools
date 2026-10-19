package vault

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DPAPIProtector uses the current Windows logon identity. It never selects
// CRYPTPROTECT_LOCAL_MACHINE or invokes a credential/UI prompt.
type DPAPIProtector struct{ VaultID string }

func (p DPAPIProtector) Identity() ProtectionIdentity {
	return ProtectionIdentity{Backend: "dpapi", VaultID: p.VaultID}
}

func (p DPAPIProtector) Wrap(ctx context.Context, key []byte) ([]byte, error) {
	b, err := osKeyPayload("dpapi", p.VaultID, key)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	return p.crypt(ctx, b, false)
}

func (p DPAPIProtector) Unwrap(ctx context.Context, wrapped []byte) ([]byte, error) {
	if !validID(p.VaultID) || len(wrapped) == 0 || len(wrapped) > MaxKeySlotBytes {
		return nil, ErrInvalid
	}
	b, err := p.crypt(ctx, wrapped, true)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	return osKeyFromPayload("dpapi", p.VaultID, b)
}

func (p DPAPIProtector) crypt(ctx context.Context, b []byte, decrypt bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entropy := []byte("devtools/vault/dpapi/v1/" + p.VaultID)
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	extra := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, &extra, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, &extra, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	runtime.KeepAlive(b)
	runtime.KeepAlive(entropy)
	if out.Data != nil {
		defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
		// OS-owned plaintext is wiped before LocalFree. Its size comes from DPAPI.
		if decrypt {
			defer clear(unsafe.Slice(out.Data, int(out.Size)))
		}
	}
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, ErrDenied
		}
		if errors.Is(err, windows.ERROR_PASSWORD_RESTRICTION) {
			return nil, ErrInteractionRequired
		}
		if decrypt {
			return nil, ErrIntegrity
		}
		return nil, ErrUnavailable
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if out.Data == nil || out.Size == 0 || out.Size > MaxKeySlotBytes {
		return nil, ErrIntegrity
	}
	return bytes.Clone(unsafe.Slice(out.Data, int(out.Size))), nil
}
