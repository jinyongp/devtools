package truststore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/ca"
	"golang.org/x/sys/windows"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "__trust-check" {
		body, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil {
			os.Exit(90)
		}
		_ = json.NewEncoder(os.Stdout).Encode(CheckPublic(body))
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func windowsAuthority(t *testing.T) (System, *ca.Authority) {
	t.Helper()
	if os.Getenv("DEVTOOLS_TEST_SYSTEM_NETWORK") != "1" {
		t.Skip("requires disposable native OS store fixture")
	}
	data := filepath.Join(t.TempDir(), "data")
	authority, _, failure := (ca.Store{Data: data}).Init(context.Background())
	if failure != nil {
		t.Fatal(failure)
	}
	return System{Directory: filepath.Join(data, "trust")}, authority
}
func userRootContains(t *testing.T, authority *ca.Authority) bool {
	t.Helper()
	store, err := openUserRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CertCloseStore(store, 0)
	certificate, err := findPublicCertificate(store, authority.Certificate.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if certificate != nil {
		windows.CertFreeCertificateContext(certificate)
		return true
	}
	return false
}
func removeFixtureCertificate(t *testing.T, authority *ca.Authority) {
	t.Helper()
	store, err := openUserRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CertCloseStore(store, 0)
	certificate, err := findPublicCertificate(store, authority.Certificate.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if certificate != nil {
		if err = windows.CertDeleteCertificateFromStore(certificate); err != nil {
			t.Fatal(err)
		}
	}
}
func TestWindowsCurrentUserTrustOwnedRoundtripAndIdempotency(t *testing.T) {
	system, authority := windowsAuthority(t)
	t.Cleanup(func() { removeFixtureCertificate(t, authority) })
	for range 2 {
		if err := system.Apply(context.Background(), Request{Action: "trust", Certificate: authority.PEM()}); err != nil {
			t.Fatal(err)
		}
	}
	if !userRootContains(t, authority) {
		t.Fatal("CA missing from CurrentUser Root")
	}
	status, failure := Check(context.Background(), authority)
	if failure != nil || !status.Known || !status.Trusted {
		t.Fatalf("fresh trust verification: %+v %v", status, failure)
	}
	for range 2 {
		if err := system.Apply(context.Background(), Request{Action: "untrust", Certificate: authority.PEM()}); err != nil {
			t.Fatal(err)
		}
	}
	if userRootContains(t, authority) {
		t.Fatal("owned CA remained trusted")
	}
	status, failure = Check(context.Background(), authority)
	if failure != nil || !status.Known || status.Trusted {
		t.Fatalf("fresh untrust verification: %+v %v", status, failure)
	}
}
func TestWindowsCurrentUserTrustPreservesExternalCertificate(t *testing.T) {
	system, authority := windowsAuthority(t)
	store, err := openUserRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CertCloseStore(store, 0)
	certificate, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING, &authority.Certificate.Raw[0], uint32(len(authority.Certificate.Raw)))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CertFreeCertificateContext(certificate)
	if err = windows.CertAddCertificateContextToStore(store, certificate, windows.CERT_STORE_ADD_NEW, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeFixtureCertificate(t, authority) })
	if err = system.Apply(context.Background(), Request{Action: "trust", Certificate: authority.PEM()}); err != nil {
		t.Fatal(err)
	}
	if err = system.Apply(context.Background(), Request{Action: "untrust", Certificate: authority.PEM()}); err == nil {
		t.Fatal("external certificate deletion authorized")
	}
	if !userRootContains(t, authority) {
		t.Fatal("external CurrentUser certificate was deleted")
	}
}
func TestWindowsCurrentUserTrustRecoversLostMutationResponses(t *testing.T) {
	for _, phase := range []string{"prepared", "added", "owned", "removing", "removed"} {
		t.Run(phase, func(t *testing.T) {
			system, authority := windowsAuthority(t)
			t.Cleanup(func() { removeFixtureCertificate(t, authority) })
			action := "trust"
			if phase == "removing" || phase == "removed" {
				if err := system.Apply(context.Background(), Request{Action: "trust", Certificate: authority.PEM()}); err != nil {
					t.Fatal(err)
				}
				action = "untrust"
			}
			system.after = func(at string) error {
				if at == phase {
					return errors.New("lost helper response")
				}
				return nil
			}
			if err := system.Apply(context.Background(), Request{Action: action, Certificate: authority.PEM()}); err == nil {
				t.Fatal("fault checkpoint not reached")
			}
			system.after = nil
			if err := system.Apply(context.Background(), Request{Action: action, Certificate: authority.PEM()}); err != nil {
				t.Fatal(err)
			}
			if got := userRootContains(t, authority); got != (action == "trust") {
				t.Fatalf("lost-response reconciliation state=%v", got)
			}
			if action == "trust" {
				if err := system.Apply(context.Background(), Request{Action: "untrust", Certificate: authority.PEM()}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestWindowsCurrentUserTrustRefusesOwnershipAfterExternalReplacement(t *testing.T) {
	system, authority := windowsAuthority(t)
	t.Cleanup(func() { removeFixtureCertificate(t, authority) })
	if err := system.Apply(context.Background(), Request{Action: "trust", Certificate: authority.PEM()}); err != nil {
		t.Fatal(err)
	}
	removeFixtureCertificate(t, authority)
	store, err := openUserRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CertCloseStore(store, 0)
	certificate, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING, &authority.Certificate.Raw[0], uint32(len(authority.Certificate.Raw)))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CertFreeCertificateContext(certificate)
	if err = windows.CertAddCertificateContextToStore(store, certificate, windows.CERT_STORE_ADD_NEW, nil); err != nil {
		t.Fatal(err)
	}
	if err = system.Apply(context.Background(), Request{Action: "untrust", Certificate: authority.PEM()}); err == nil {
		t.Fatal("stale ownership receipt deleted replacement")
	}
	if !userRootContains(t, authority) {
		t.Fatal("external replacement removed")
	}
}
func TestWindowsCurrentUserTrustCancellationAndIsolation(t *testing.T) {
	system, authority := windowsAuthority(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := system.Apply(ctx, Request{Action: "trust", Certificate: authority.PEM()}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	t.Setenv("DEVTOOLS_ISOLATED_ROOT", t.TempDir())
	if err := system.Apply(context.Background(), Request{Action: "trust", Certificate: authority.PEM()}); err == nil {
		t.Fatal("isolated trust mutation allowed")
	}
	if userRootContains(t, authority) {
		t.Fatal("canceled/isolated request changed Root store")
	}
}
