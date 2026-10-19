package vault

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

type blockedSessionResponse struct {
	net.Conn
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockedSessionResponse) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte(`"key":`)) {
		c.once.Do(func() { close(c.started); <-c.release })
	}
	return c.Conn.Write(b)
}
func testSessionConnection(t *testing.T, ctx context.Context, e SessionEndpoint, s *KeySession, a *sessionAdmissions, block bool) (net.Conn, sessionHello, *blockedSessionResponse) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { server.Close(); client.Close() })
	var wrapper *blockedSessionResponse
	var connection sessionConnection = server
	if block {
		wrapper = &blockedSessionResponse{Conn: server, started: make(chan struct{}), release: make(chan struct{})}
		connection = wrapper
	}
	go func() { defer server.Close(); serveSessionConnection(ctx, connection, e, s, a, func() {}) }()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	var hello sessionHello
	if err := readSessionFrame(client, &hello); err != nil {
		t.Fatal(err)
	}
	return client, hello, wrapper
}
func testSessionRequest(e SessionEndpoint, h sessionHello, op string) sessionRequest {
	return sessionRequest{Protocol: e.Protocol, VaultID: e.VaultID, SessionID: e.SessionID, Challenge: h.Challenge, LockEpoch: h.LockEpoch, Operation: op}
}
func TestSessionLockDrainsResponseAndRejectsOldUnlock(t *testing.T) {
	dir, p, _ := sessionFixture(t)
	s := NewKeySession(dir)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	id, _ := NewID()
	e := SessionEndpoint{Protocol: SessionProtocol, VaultID: p.VaultID, SessionID: id}
	a := &sessionAdmissions{}
	// This unlock connection was established before the later manual lock.
	old, oldHello, _ := testSessionConnection(t, ctx, e, s, a, false)
	use, useHello, blocked := testSessionConnection(t, ctx, e, s, a, true)
	if err := writeSessionFrame(use, testSessionRequest(e, useHello, "use")); err != nil {
		t.Fatal(err)
	}
	used := make(chan error, 1)
	go func() {
		var response sessionResponse
		err := readSessionFrame(use, &response)
		clear(response.Key)
		used <- err
	}()
	select {
	case <-blocked.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	lock, lockHello, _ := testSessionConnection(t, ctx, e, s, a, false)
	if err := writeSessionFrame(lock, testSessionRequest(e, lockHello, "lock")); err != nil {
		t.Fatal(err)
	}
	locked := make(chan error, 1)
	go func() {
		var response sessionResponse
		err := readSessionFrame(lock, &response)
		if err == nil {
			err = sessionError(response.Error)
		}
		locked <- err
	}()
	for s.Status().State != LockedManual {
		if err := waitSession(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// Durable intent is visible while the response is still blocked, and the
	// lock acknowledgement cannot overtake that already admitted delivery.
	select {
	case err := <-locked:
		t.Fatal("acknowledged before drain", err)
	default:
	}
	close(blocked.release)
	if err := <-used; err != nil {
		t.Fatal(err)
	}
	if err := <-locked; err != nil {
		t.Fatal(err)
	}
	request := testSessionRequest(e, oldHello, "unlock")
	request.Passphrase = p.Passphrase.CopyBytes()
	defer clear(request.Passphrase)
	if err := writeSessionFrame(old, request); err != nil {
		t.Fatal(err)
	}
	var response sessionResponse
	if err := readSessionFrame(old, &response); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(sessionError(response.Error), ErrLockPending) || s.Status().State != LockedManual {
		t.Fatal("old unlock cleared manual lock")
	}
}

func TestSessionChallengeMismatchCannotUseKey(t *testing.T) {
	dir, p, _ := sessionFixture(t)
	s := NewKeySession(dir)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	id, _ := NewID()
	e := SessionEndpoint{Protocol: SessionProtocol, VaultID: p.VaultID, SessionID: id}
	c, h, _ := testSessionConnection(t, ctx, e, s, &sessionAdmissions{}, false)
	req := testSessionRequest(e, h, "use")
	req.Challenge = "00000000000000000000000000000000"
	if err := writeSessionFrame(c, req); err != nil {
		t.Fatal(err)
	}
	var response sessionResponse
	if err := readSessionFrame(c, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Key) > 0 || response.Error != ErrSessionProtocol.Error() {
		t.Fatal("challenge bypass")
	}
}
