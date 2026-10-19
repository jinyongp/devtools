package vault

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionSameUserRawClientCanReceiveKey(t *testing.T) {
	dir, protector, want := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := (SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Retire(context.Background())
	if err = client.Unlock(ctx, protector.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	// Any same-user program able to read the endpoint can implement this wire
	// exchange. The protocol authenticates users, not application identity.
	conn, err := dialSession(ctx, client.Endpoint.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	var hello sessionHello
	if err = readSessionFrame(conn, &hello); err != nil {
		t.Fatal(err)
	}
	if err = writeSessionFrame(conn, testSessionRequest(client.Endpoint, hello, "use")); err != nil {
		t.Fatal(err)
	}
	var response sessionResponse
	if err = readSessionFrame(conn, &response); err != nil {
		t.Fatal(err)
	}
	defer clear(response.Key)
	expected := want.CopyBytes()
	defer clear(expected)
	if response.Error != "" || !bytes.Equal(response.Key, expected) {
		t.Fatal("same-user trust boundary changed")
	}
}

func TestSessionConnectionFloodIsBoundedAndRecovers(t *testing.T) {
	dir, _, _ := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := (SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Retire(context.Background())
	held := []sessionConnection{}
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	// Reading each hello establishes that the real daemon has admitted the
	// connection; no sleeps or assumptions about accept scheduling are needed.
	for i := 0; i < maxSessionConnections; i++ {
		c, err := dialSession(ctx, client.Endpoint.Endpoint)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
		c.SetDeadline(time.Now().Add(3 * time.Second))
		var hello sessionHello
		if err := readSessionFrame(c, &hello); err != nil {
			t.Fatal(err)
		}
	}
	extra, err := dialSession(ctx, client.Endpoint.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	extra.SetDeadline(time.Now().Add(3 * time.Second))
	var hello sessionHello
	err = readSessionFrame(extra, &hello)
	extra.Close()
	var timeout net.Error
	if err == nil || errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatal("excess connection was not rejected promptly", err)
	}
	for _, c := range held {
		c.Close()
	}
	held = nil
	// After closing hostile connections, accept workers may still be unwinding.
	for {
		if _, err := client.Status(ctx); err == nil {
			break
		}
		if err := waitSession(ctx); err != nil {
			t.Fatal("capacity did not recover", err)
		}
	}
}

type countedUnwrap struct {
	Protector
	active, peak, calls *atomic.Int32
}

func (p countedUnwrap) Unwrap(ctx context.Context, b []byte) ([]byte, error) {
	n := p.active.Add(1)
	defer p.active.Add(-1)
	p.calls.Add(1)
	for old := p.peak.Load(); n > old; old = p.peak.Load() {
		if p.peak.CompareAndSwap(old, n) {
			break
		}
	}
	return p.Protector.Unwrap(ctx, b)
}
func TestSessionConcurrentWrongPassphrasesSerializeRealKDF(t *testing.T) {
	dir, _, _ := sessionFixture(t)
	session := NewKeySession(dir)
	defer session.Close()
	var active, peak, calls atomic.Int32
	session.protector = func(k KeyFile, pass *Material, interactive bool) (Protector, error) {
		return countedUnwrap{PassphraseProtector{VaultID: k.VaultID, Passphrase: pass}, &active, &peak, &calls}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const workers = 8
	ready := make(chan struct{}, workers)
	start := make(chan struct{})
	failures := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pass := NewMaterial([]byte("synthetic-wrong-password"))
			defer pass.Destroy()
			ready <- struct{}{}
			<-start
			failures <- session.Unlock(ctx, pass, false)
		}()
	}
	for i := 0; i < workers; i++ {
		<-ready
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		if !errors.Is(err, ErrIntegrity) {
			t.Fatal("wrong password result", err)
		}
	}
	if calls.Load() != workers || peak.Load() != 1 || active.Load() != 0 {
		t.Fatal("concurrent KDF work was unbounded", calls.Load(), peak.Load(), active.Load())
	}
	if session.Status().State == Unlocked {
		t.Fatal("invalid credentials unlocked session")
	}
}
