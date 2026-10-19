package vault

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type stalledSessionProtector struct {
	Protector
	entered chan struct{}
	release chan struct{}
}

func (p stalledSessionProtector) Unwrap(ctx context.Context, b []byte) ([]byte, error) {
	close(p.entered)
	<-p.release
	// Model an OS operation that produces a late result despite cancellation.
	return p.Protector.Unwrap(context.Background(), b)
}

func TestSessionLockAndStatusDoNotWaitForNativeResult(t *testing.T) {
	id, _ := NewID()
	wrapping := NewMaterial(bytes.Repeat([]byte{0x63}, 32))
	defer wrapping.Destroy()
	p := sessionTestProtector{id: id, key: wrapping}
	dir := filepath.Join(t.TempDir(), "vault")
	key, err := (KeyFileStore{Directory: dir}).Initialize(context.Background(), id, "dpapi", p)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	s := NewKeySession(dir)
	defer s.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	s.protector = func(KeyFile, *Material, bool) (Protector, error) {
		return stalledSessionProtector{p, entered, release}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	used := make(chan error, 1)
	go func() { key, err := s.Use(ctx); key.Destroy(); used <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	status := make(chan SessionStatus, 1)
	go func() { status <- s.Status() }()
	select {
	case got := <-status:
		if got.State != Locked {
			t.Fatal("unexpected state")
		}
	case <-time.After(time.Second):
		t.Fatal("status waited for OS result")
	}
	locked := make(chan error, 1)
	go func() { locked <- s.Lock(ctx) }()
	select {
	case err := <-locked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lock waited for OS result")
	}
	close(release)
	released = true
	if err := <-used; !errors.Is(err, ErrLockPending) {
		t.Fatal("late result accepted", err)
	}
	if s.Status().State != LockedManual {
		t.Fatal("late OS result cleared manual lock")
	}
}

type cancelSessionProtector struct {
	Protector
	entered  chan struct{}
	canceled chan struct{}
}

func (p cancelSessionProtector) Unwrap(ctx context.Context, b []byte) ([]byte, error) {
	close(p.entered)
	<-ctx.Done()
	close(p.canceled)
	return nil, ctx.Err()
}
func TestSessionDisconnectCancelsNativeWork(t *testing.T) {
	id, _ := NewID()
	wrapping := NewMaterial(bytes.Repeat([]byte{0x37}, 32))
	defer wrapping.Destroy()
	p := sessionTestProtector{id: id, key: wrapping}
	dir := filepath.Join(t.TempDir(), "vault")
	key, err := (KeyFileStore{Directory: dir}).Initialize(context.Background(), id, "dpapi", p)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	s := NewKeySession(dir)
	defer s.Close()
	entered, canceled := make(chan struct{}), make(chan struct{})
	s.protector = func(KeyFile, *Material, bool) (Protector, error) {
		return cancelSessionProtector{p, entered, canceled}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessionID, _ := NewID()
	e := SessionEndpoint{Protocol: SessionProtocol, VaultID: id, SessionID: sessionID}
	c, h, _ := testSessionConnection(t, ctx, e, s, &sessionAdmissions{}, false)
	if err := writeSessionFrame(c, testSessionRequest(e, h, "use")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	c.Close()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("abandoned client left OS work running")
	}
}
