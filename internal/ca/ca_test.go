package ca

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestReadDoesNotInitializeAndConcurrentInitHasOneRoot(t *testing.T) {
	s := Store{Data: filepath.Join(t.TempDir(), "data")}
	if status, e := s.Status(); e != nil || status.Present {
		t.Fatal(status, e)
	}
	if _, e := os.Stat(s.Data); !os.IsNotExist(e) {
		t.Fatal("status created storage", e)
	}
	var wg sync.WaitGroup
	fingerprints := make(chan string, 12)
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			a, _, e := s.Init(context.Background())
			if e != nil {
				t.Error(e)
				return
			}
			fingerprints <- a.Fingerprint()
		})
	}
	wg.Wait()
	close(fingerprints)
	first := ""
	for fp := range fingerprints {
		if first == "" {
			first = fp
		}
		if fp != first {
			t.Fatal("multiple roots")
		}
	}
	info, e := os.Stat(filepath.Join(s.root(), "material.json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
}

func TestCorruptAndExpiredRootsAreNotReplaced(t *testing.T) {
	s := Store{Data: t.TempDir()}
	a, _, e := s.Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(s.root(), "material.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return a.Certificate.NotAfter.Add(time.Hour) }
	if _, _, e := s.Init(context.Background()); e == nil || e.Code != "ca_expired" {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("expired root replaced")
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Now = nil
	if _, _, e := s.Init(context.Background()); e == nil || e.Code != "invalid_ca" {
		t.Fatal(e)
	}
	after, _ = os.ReadFile(path)
	if string(after) != "corrupt" {
		t.Fatal("corrupt root replaced")
	}
}

func TestLeafChainAndTLSDoNotRequireOSTrust(t *testing.T) {
	s := Store{Data: t.TempDir()}
	a, _, e := s.Init(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	leaf, e := a.Leaf("app.localhost", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	pool := x509.NewCertPool()
	pool.AddCert(a.Certificate)
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{DNSName: "app.localhost", Roots: pool}); err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{DNSName: "other.localhost", Roots: pool}); err == nil {
		t.Fatal("wrong SAN accepted")
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "app.localhost", MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: time.Second}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal(response.StatusCode)
	}
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{DNSName: "app.localhost"}); err == nil {
		t.Fatal("ephemeral root unexpectedly trusted by OS")
	}
	path := filepath.Join(t.TempDir(), "ca.crt")
	if e := Export(a, path); e != nil {
		t.Fatal(e)
	}
	if e := Export(a, path); e == nil || e.Code != "output_exists" {
		t.Fatal(e)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(a.PEM()) {
		t.Fatal(err)
	}
}
