package truststore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/tasks"
	"golang.org/x/sys/windows"
)

// A user-defined context property binds a NEW certificate to its private receipt.
// Add-new copies the property with the certificate, so response loss is recoverable
// without claiming or deleting a certificate that another actor had already added.
const ownershipProperty = 0x8000
const ownershipPrefix = "devtools-trust-v1:"

type trustReceipt struct {
	Version     int    `json:"version"`
	Fingerprint string `json:"fingerprint"`
	Nonce       string `json:"nonce"`
	State       string `json:"state"`
}

var setCertificateProperty = windows.NewLazySystemDLL("crypt32.dll").NewProc("CertSetCertificateContextProperty")
var getCertificateProperty = windows.NewLazySystemDLL("crypt32.dll").NewProc("CertGetCertificateContextProperty")

func setOwnership(cert *windows.CertContext, nonce string) error {
	body := []byte(ownershipPrefix + nonce)
	blob := windows.CryptDataBlob{Size: uint32(len(body)), Data: &body[0]}
	result, _, err := setCertificateProperty.Call(uintptr(unsafe.Pointer(cert)), ownershipProperty, 0, uintptr(unsafe.Pointer(&blob)))
	runtime.KeepAlive(body)
	if result == 0 {
		return err
	}
	return nil
}
func hasOwnership(cert *windows.CertContext, nonce string) bool {
	buffer := make([]byte, 256)
	size := uint32(len(buffer))
	result, _, _ := getCertificateProperty.Call(uintptr(unsafe.Pointer(cert)), ownershipProperty, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	return result != 0 && size <= uint32(len(buffer)) && bytes.Equal(buffer[:size], []byte(ownershipPrefix+nonce))
}
func openUserRoot() (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString("Root")
	if err != nil {
		return 0, err
	}
	return windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM_W, 0, 0, windows.CERT_SYSTEM_STORE_CURRENT_USER, uintptr(unsafe.Pointer(name)))
}
func findPublicCertificate(store windows.Handle, raw []byte) (*windows.CertContext, error) {
	var previous *windows.CertContext
	for range 100000 {
		current, err := windows.CertEnumCertificatesInStore(store, previous)
		if err != nil {
			if errors.Is(err, windows.Errno(0x80092004)) {
				return nil, nil
			}
			return nil, err
		}
		if current.Length <= 1<<20 && bytes.Equal(unsafe.Slice(current.EncodedCert, int(current.Length)), raw) {
			return current, nil
		}
		previous = current
	}
	if previous != nil {
		windows.CertFreeCertificateContext(previous)
	}
	return nil, errors.New("certificate store enumeration exceeded limit")
}
func validTrustReceipt(receipt trustReceipt, fingerprint string) bool {
	nonce, err := hex.DecodeString(receipt.Nonce)
	return receipt.Version == 1 && receipt.Fingerprint == fingerprint && err == nil && len(nonce) == 32 && (receipt.State == "prepared" || receipt.State == "owned" || receipt.State == "removing")
}
func (s System) windows(ctx context.Context, request Request, fingerprint string, certificate *x509.Certificate) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if os.Getenv("DEVTOOLS_ISOLATED_ROOT") != "" {
		return errors.New("trust changes are unavailable in isolated mode")
	}
	directory := s.Directory
	if directory == "" {
		dirs, err := paths.Current()
		if err != nil {
			return err
		}
		if dirs.IsolatedRoot != "" {
			return errors.New("isolated trust mutation")
		}
		directory = filepath.Join(dirs.Data, "trust")
	}
	unlock, err := tasks.Lock(ctx, filepath.Join(directory, "root.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	root, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return err
	}
	defer root.Close()
	name := fingerprint + ".json"
	var receipt trustReceipt
	body, err := root.ReadFile(name, 16384)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if exists {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&receipt) != nil || decoder.Decode(new(any)) != io.EOF || !validTrustReceipt(receipt, fingerprint) {
			return errors.New("invalid trust ownership receipt")
		}
	}
	save := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		body, _ := json.Marshal(receipt)
		return root.WriteFile(ctx, name, body, true)
	}
	checkpoint := func(phase string) error {
		if s.after != nil {
			return s.after(phase)
		}
		return nil
	}
	store, err := openUserRoot()
	if err != nil {
		return err
	}
	defer windows.CertCloseStore(store, 0)
	current, err := findPublicCertificate(store, certificate.Raw)
	if err != nil {
		return err
	}
	defer func() {
		if current != nil {
			windows.CertFreeCertificateContext(current)
		}
	}()
	if current != nil && (!exists || !hasOwnership(current, receipt.Nonce)) {
		// Existing external trust is never converted into owned trust or removed.
		if exists && receipt.State == "prepared" {
			// Add-new lost a race to an external copy, rather than creating ours.
			if err := root.RemoveFile(ctx, name); err != nil {
				return err
			}
			exists = false
		}
		if request.Action == "trust" && !exists {
			return nil
		}
		return errors.New("certificate ownership is not confirmed; external trust preserved")
	}
	if exists && receipt.State == "removing" {
		if current != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			err = windows.CertDeleteCertificateFromStore(current)
			current = nil
			if err != nil {
				return err
			}
		}
		if err = root.RemoveFile(ctx, name); err != nil {
			return err
		}
		exists = false
	}
	if request.Action == "untrust" {
		if !exists {
			return nil
		}
		receipt.State = "removing"
		if err = save(); err != nil {
			return err
		}
		if err = checkpoint("removing"); err != nil {
			return err
		}
		if current != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			err = windows.CertDeleteCertificateFromStore(current)
			current = nil
			if err != nil {
				return err
			}
		}
		if err = checkpoint("removed"); err != nil {
			return err
		}
		return root.RemoveFile(ctx, name)
	}
	if !exists {
		nonce := make([]byte, 32)
		if _, err = rand.Read(nonce); err != nil {
			return err
		}
		receipt = trustReceipt{Version: 1, Fingerprint: fingerprint, Nonce: hex.EncodeToString(nonce), State: "prepared"}
		if err = save(); err != nil {
			return err
		}
		if err = checkpoint("prepared"); err != nil {
			return err
		}
	}
	if current == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		candidate, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING, &certificate.Raw[0], uint32(len(certificate.Raw)))
		if err != nil {
			return err
		}
		defer windows.CertFreeCertificateContext(candidate)
		if err = setOwnership(candidate, receipt.Nonce); err != nil {
			return err
		}
		if err = windows.CertAddCertificateContextToStore(store, candidate, windows.CERT_STORE_ADD_NEW, &current); err != nil {
			return err
		}
		if !hasOwnership(current, receipt.Nonce) {
			return errors.New("new certificate ownership property was not persisted; receipt retained")
		}
		if err = checkpoint("added"); err != nil {
			return err
		}
	}
	receipt.State = "owned"
	if err = save(); err != nil {
		return err
	}
	return checkpoint("owned")
}
