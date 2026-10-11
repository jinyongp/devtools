// Package ca manages a private, host-local certificate authority. Reading never
// initializes or replaces it; explicit HTTPS preparation calls Init.
package ca

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Store struct {
	Data string
	Now  func() time.Time
}
type Authority struct {
	Certificate *x509.Certificate
	key         crypto.Signer
}
type material struct {
	Version     int    `json:"version"`
	Certificate []byte `json:"certificate"`
	Key         []byte `json:"key"`
}
type Status struct {
	Present     bool       `json:"present"`
	Fingerprint string     `json:"fingerprint"`
	NotAfter    *time.Time `json:"not_after"`
}

func failure(code string) *protocol.Error {
	return protocol.NewError(code, "Cannot use the local certificate authority; existing material has been preserved.", 3, nil)
}
func (s Store) root() string { return filepath.Join(s.Data, "ca") }
func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}
func (s Store) Read() (*Authority, *protocol.Error) {
	return s.read(true)
}

// ReadForRemoval validates identity and key integrity even after expiry.
func (s Store) ReadForRemoval() (*Authority, *protocol.Error) { return s.read(false) }
func (s Store) read(checkExpiry bool) (*Authority, *protocol.Error) {
	info, e := os.Lstat(s.root())
	if errors.Is(e, os.ErrNotExist) {
		return nil, failure("ca_not_found")
	}
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, failure("invalid_ca")
	}
	var m material
	e = tasks.ReadPrivate(filepath.Join(s.root(), "material.json"), &m)
	if errors.Is(e, os.ErrNotExist) {
		return nil, failure("ca_not_found")
	}
	if e != nil || m.Version != 1 {
		return nil, failure("invalid_ca")
	}
	cert, e := x509.ParseCertificate(m.Certificate)
	if e != nil {
		return nil, failure("invalid_ca")
	}
	parsed, e := x509.ParsePKCS8PrivateKey(m.Key)
	if e != nil {
		return nil, failure("invalid_ca")
	}
	key, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, failure("invalid_ca")
	}
	pub, e := x509.MarshalPKIXPublicKey(key.Public())
	if e != nil || !bytes.Equal(pub, cert.RawSubjectPublicKeyInfo) || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 || cert.CheckSignatureFrom(cert) != nil {
		return nil, failure("invalid_ca")
	}
	now := s.now()
	if checkExpiry && (now.Before(cert.NotBefore) || !now.Before(cert.NotAfter)) {
		return nil, failure("ca_expired")
	}
	return &Authority{Certificate: cert, key: key}, nil
}

func (s Store) Remove(ctx context.Context, fingerprint string) *protocol.Error {
	unlock, e := tasks.Lock(ctx, filepath.Join(s.root(), "init.lock"))
	if e != nil {
		return failure("io_error")
	}
	defer unlock()
	authority, err := s.ReadForRemoval()
	if err != nil {
		if err.Code == "ca_not_found" {
			return nil
		}
		return err
	}
	if authority.Fingerprint() != fingerprint {
		return failure("ca_changed")
	}
	if e := os.Remove(filepath.Join(s.root(), "material.json")); e != nil {
		return failure("io_error")
	}
	d, e := os.Open(s.root())
	if e != nil {
		return failure("io_error")
	}
	defer d.Close()
	if d.Sync() != nil {
		return failure("io_error")
	}
	return nil
}
func (s Store) Init(ctx context.Context) (*Authority, bool, *protocol.Error) {
	if e := tasks.PrivateDir(s.root()); e != nil {
		return nil, false, failure("invalid_ca")
	}
	unlock, e := tasks.Lock(ctx, filepath.Join(s.root(), "init.lock"))
	if e != nil {
		return nil, false, failure("io_error")
	}
	defer unlock()
	if authority, err := s.Read(); err == nil {
		return authority, false, nil
	} else if err.Code != "ca_not_found" {
		return nil, false, err
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, false, failure("io_error")
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return nil, false, failure("io_error")
	}
	serial.Add(serial, big.NewInt(1))
	now := s.now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "devtools local root " + serial.Text(16), Organization: []string{"devtools local development"}}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return nil, false, failure("io_error")
	}
	private, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return nil, false, failure("io_error")
	}
	if e := tasks.WritePrivate(filepath.Join(s.root(), "material.json"), material{Version: 1, Certificate: der, Key: private}); e != nil {
		return nil, false, failure("io_error")
	}
	a, err := s.Read()
	return a, true, err
}
func (a *Authority) Fingerprint() string {
	sum := sha256.Sum256(a.Certificate.Raw)
	return hex.EncodeToString(sum[:])
}
func (a *Authority) PEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.Certificate.Raw})
}
func (s Store) Status() (Status, *protocol.Error) {
	a, e := s.Read()
	if e != nil {
		if e.Code == "ca_not_found" {
			return Status{}, nil
		}
		return Status{}, e
	}
	end := a.Certificate.NotAfter
	return Status{Present: true, Fingerprint: a.Fingerprint(), NotAfter: &end}, nil
}
func (a *Authority) Leaf(host string, now time.Time) (tls.Certificate, *protocol.Error) {
	if !network.ValidHost(host) {
		return tls.Certificate{}, failure("invalid_host")
	}
	if now.Before(a.Certificate.NotBefore) || !now.Before(a.Certificate.NotAfter) {
		return tls.Certificate{}, failure("ca_expired")
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return tls.Certificate{}, failure("io_error")
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return tls.Certificate{}, failure("io_error")
	}
	serial.Add(serial, big.NewInt(1))
	end := now.AddDate(0, 0, 30)
	if end.After(a.Certificate.NotAfter) {
		end = a.Certificate.NotAfter
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: now.Add(-5 * time.Minute), NotAfter: end, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, template, a.Certificate, &key.PublicKey, a.key)
	if e != nil {
		return tls.Certificate{}, failure("io_error")
	}
	leaf, e := x509.ParseCertificate(der)
	if e != nil {
		return tls.Certificate{}, failure("io_error")
	}
	return tls.Certificate{Certificate: [][]byte{der, a.Certificate.Raw}, PrivateKey: key, Leaf: leaf}, nil
}
func Export(a *Authority, path string) *protocol.Error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(e, os.ErrExist) {
		return failure("output_exists")
	}
	if e != nil {
		return failure("io_error")
	}
	_, e = f.Write(a.PEM())
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return failure("io_error")
	}
	return nil
}
