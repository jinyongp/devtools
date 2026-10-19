package vault

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCachedInspectionNeverUnwrapsOrTouchesIdle(t *testing.T) {
	id, _ := NewID()
	wrapping := NewMaterial(bytes.Repeat([]byte{0x65}, 32))
	defer wrapping.Destroy()
	protector := sessionTestProtector{id: id, key: wrapping}
	directory := filepath.Join(privateTempDir(t), "vault")
	want, err := (KeyFileStore{Directory: directory}).Initialize(context.Background(), id, "dpapi", protector)
	if err != nil {
		t.Fatal(err)
	}
	defer want.Destroy()
	session := NewKeySession(directory)
	defer session.Close()
	now := time.Now()
	session.now = func() time.Time { return now }
	calls := 0
	session.protector = func(KeyFile, *Material, bool) (Protector, error) { calls++; return protector, nil }
	if key, err := session.InspectCached(context.Background()); key != nil || !errors.Is(err, ErrLocked) || calls != 0 {
		t.Fatal("cold inspection unwrapped", err)
	}
	key, err := session.Use(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	before := session.Status()
	oldEpoch := session.lockEpoch()
	for i := 0; i < 3; i++ {
		key, err := session.InspectCached(context.Background())
		if err != nil || !sameMaterial(key, want) {
			t.Fatal(err)
		}
		key.Destroy()
	}
	after := session.Status()
	if calls != 1 || before.IdleExpiresAt == nil || after.IdleExpiresAt == nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("inspection changed idle or reauthenticated")
	}
	now = now.Add(DefaultIdleTimeout)
	if key, err := session.InspectCached(context.Background()); key != nil || !errors.Is(err, ErrLocked) || calls != 1 {
		t.Fatal("expired inspection silently reopened", err)
	}
	if err := session.Unlock(context.Background(), nil, false); err != nil {
		t.Fatal(err)
	}
	if err := session.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if key, err := session.InspectCached(context.Background()); key != nil || !errors.Is(err, ErrLocked) || calls != 2 {
		t.Fatal("manual inspection silently reopened", err)
	}
	if err := session.Unlock(context.Background(), nil, false); err != nil {
		t.Fatal(err)
	}
	if key, err := session.inspectCachedAtEpoch(context.Background(), &oldEpoch); key != nil || !errors.Is(err, ErrLockPending) {
		t.Fatal("old inspection survived lock", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if key, err := session.InspectCached(canceled); key != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("inspection ignored cancellation", err)
	}
}

func TestCachedInspectionActualIPCDoesNotExtendIdle(t *testing.T) {
	directory, p, want := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := (SessionManager{Directory: directory}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Retire(context.Background())
	if key, err := client.InspectCached(ctx); key != nil || !errors.Is(err, ErrLocked) {
		t.Fatal("cold RPC inspection", err)
	}
	if err := client.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	before, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		key, err := client.InspectCached(ctx)
		if err != nil || !sameMaterial(key, want) {
			t.Fatal(err)
		}
		key.Destroy()
	}
	after, err := client.Status(ctx)
	if err != nil || before.IdleExpiresAt == nil || after.IdleExpiresAt == nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("real IPC inspection extended idle", err)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if key, err := client.InspectCached(ctx); key != nil || !errors.Is(err, ErrLocked) {
		t.Fatal("manual lock bypassed by inspection", err)
	}
}

func TestLockDrainsCachedInspectionResponse(t *testing.T) {
	directory, p, _ := sessionFixture(t)
	session := NewKeySession(directory)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := session.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	sid, _ := NewID()
	endpoint := SessionEndpoint{Protocol: SessionProtocol, VaultID: p.VaultID, SessionID: sid}
	admissions := &sessionAdmissions{}
	inspect, hello, blocked := testSessionConnection(t, ctx, endpoint, session, admissions, true)
	var released sync.Once
	unblock := func() { released.Do(func() { close(blocked.release) }) }
	defer unblock()
	if err := writeSessionFrame(inspect, testSessionRequest(endpoint, hello, "inspect")); err != nil {
		t.Fatal(err)
	}
	inspected := make(chan error, 1)
	go func() {
		var response sessionResponse
		err := readSessionFrame(inspect, &response)
		if err == nil && (len(response.Key) != 32 || response.Ticket != nil) {
			err = ErrIntegrity
		}
		clear(response.Key)
		inspected <- err
	}()
	select {
	case <-blocked.started:
	case <-ctx.Done():
		t.Fatal("inspection response did not reach delivery boundary")
	}
	lock, lockHello, _ := testSessionConnection(t, ctx, endpoint, session, admissions, false)
	if err := writeSessionFrame(lock, testSessionRequest(endpoint, lockHello, "lock")); err != nil {
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
	for session.Status().State != LockedManual {
		if err := waitSession(ctx); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-locked:
		t.Fatal("lock acknowledged undrained inspection", err)
	default:
	}
	unblock()
	if err := <-inspected; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-locked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("inspection was not drained")
	}
}
