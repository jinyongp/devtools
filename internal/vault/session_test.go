package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "__vault-rewrap-fixture" {
		runRewrapFixture()
		os.Exit(0)
	}
	if len(os.Args) == 3 && os.Args[1] == "__vault-forward-fixture" {
		runForwardFixture()
		os.Exit(0)
	}
	if len(os.Args) == 4 && os.Args[1] == "__vault-launch-fixture" {
		runLaunchFixture()
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "__vault-native-unwrap" {
		if NativeUnlockHelper(context.Background(), os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	// Exercise the manager through a real child process of this test binary.
	if len(os.Args) == 3 && os.Args[1] == "__vault-session" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if ServeKeySession(ctx, os.Args[2]) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func sessionFixture(t *testing.T) (string, PassphraseProtector, *Material) {
	t.Helper()
	base, err := os.MkdirTemp("", "dvs-")
	if err != nil {
		t.Fatal(err)
	}
	temporary := base
	t.Cleanup(func() { os.RemoveAll(temporary) })
	// MkdirTemp does not establish a protected user-only ACL on Windows.
	// Use the production filesystem boundary for the actual data root.
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	base = filepath.Join(base, "private")
	root, err := platformfs.OpenRoot(base, true)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	directory := filepath.Join(base, "vault")
	p := protectorFixture(t)
	key, err := (KeyFileStore{Directory: directory}).Initialize(context.Background(), p.VaultID, "passphrase", p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(key.Destroy)
	return directory, p, key
}
func sameMaterial(a, b *Material) bool {
	x, y := a.CopyBytes(), b.CopyBytes()
	defer clear(x)
	defer clear(y)
	return bytes.Equal(x, y)
}
func TestSessionIdleManualLockAndRestart(t *testing.T) {
	directory, p, want := sessionFixture(t)
	s := NewKeySession(directory)
	defer s.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := s.Use(ctx); !errors.Is(err, ErrLocked) {
		t.Fatal("passphrase required", err)
	}
	if err := s.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(7 * time.Hour)
	s.Status()
	now = now.Add(time.Hour)
	if s.Status().State != Locked {
		t.Fatal("poll extended idle")
	}
	if err := s.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(7 * time.Hour)
	got, err := s.Use(ctx)
	if err != nil || !sameMaterial(got, want) {
		t.Fatal("use", err)
	}
	got.Destroy()
	now = now.Add(7 * time.Hour)
	if s.Status().State != Unlocked {
		t.Fatal("real use did not extend idle")
	}
	if err := s.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Use(ctx); !errors.Is(err, ErrLocked) {
		t.Fatal("manual lock bypassed", err)
	}
	s.Close()
	restarted := NewKeySession(directory)
	defer restarted.Close()
	if restarted.Status().State != LockedManual {
		t.Fatal("manual lock lost on restart")
	}
	wrong := NewMaterial([]byte("wrong synthetic passphrase"))
	defer wrong.Destroy()
	if err := restarted.Unlock(ctx, wrong, false); err == nil {
		t.Fatal("bad unlock accepted")
	}
	if restarted.Status().State != LockedManual {
		t.Fatal("failed unlock cleared latch")
	}
	if err := restarted.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	if restarted.Status().State != Unlocked {
		t.Fatal("explicit unlock failed")
	}
}

// This fixture substitutes an OS protector only for state/idle tests. Its
// wrapping uses a synthetic AEAD key; it does not validate any native key store.
type sessionTestProtector struct {
	id  string
	key *Material
}

func (p sessionTestProtector) Identity() ProtectionIdentity {
	return ProtectionIdentity{Backend: "dpapi", VaultID: p.id}
}
func (p sessionTestProtector) Wrap(ctx context.Context, b []byte) ([]byte, error) {
	k := NewMaterial(b)
	defer k.Destroy()
	return WrapKeyset(p.key, p.id, "0123456789abcdef0123456789abcdef", 1, k)
}
func (p sessionTestProtector) Unwrap(ctx context.Context, b []byte) ([]byte, error) {
	k, err := UnwrapKeyset(p.key, p.id, "0123456789abcdef0123456789abcdef", 1, b)
	if err != nil {
		return nil, err
	}
	defer k.Destroy()
	return k.CopyBytes(), nil
}
func TestSessionSilentOSReopenAndKeySlotChange(t *testing.T) {
	dir := filepath.Join(privateTempDir(t), "vault")
	id, _ := NewID()
	wrapping := NewMaterial(bytes.Repeat([]byte{0x31}, 32))
	defer wrapping.Destroy()
	p := sessionTestProtector{id: id, key: wrapping}
	want, err := (KeyFileStore{Directory: dir}).Initialize(context.Background(), id, "dpapi", p)
	if err != nil {
		t.Fatal(err)
	}
	defer want.Destroy()
	s := NewKeySession(dir)
	defer s.Close()
	calls := 0
	s.protector = func(k KeyFile, _ *Material, interactive bool) (Protector, error) {
		calls++
		if interactive {
			t.Fatal("silent use requested UI")
		}
		return p, nil
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	if s.Status().State != Locked || calls != 0 {
		t.Fatal("status silently unlocked")
	}
	key, err := s.Use(context.Background())
	if err != nil || !sameMaterial(key, want) {
		t.Fatal(err)
	}
	key.Destroy()
	now = now.Add(DefaultIdleTimeout)
	if s.Status().State != Locked || calls != 1 {
		t.Fatal("idle state")
	}
	key, err = s.Use(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	if calls != 2 {
		t.Fatal("OS key did not reopen")
	}
	if err = s.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Use(context.Background()); !errors.Is(err, ErrLocked) || calls != 2 {
		t.Fatal("manual lock auto reopened")
	}
	if err = s.Unlock(context.Background(), nil, false); err != nil {
		t.Fatal(err)
	}
	r, _ := platformfs.OpenRoot(dir, false)
	defer r.Close()
	original, err := r.ReadFile(keyFilename, MaxKeySlotBytes*2)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.WriteFile(context.Background(), keyFilename, []byte(`{"invalid":true}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Use(context.Background()); err == nil || s.Status().State != RecoveryRequired {
		t.Fatal("corrupt key slot used")
	}
	if err = r.WriteFile(context.Background(), keyFilename, original, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Use(context.Background()); !errors.Is(err, ErrRecovery) {
		t.Fatal("recovery silently reopened", err)
	}
	if err = s.Unlock(context.Background(), nil, false); err != nil {
		t.Fatal("explicit recovery unlock", err)
	}
}
func TestSessionSeparateProcessAndDurableLock(t *testing.T) {
	dir, p, want := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	manager := SessionManager{Directory: dir}
	c, err := manager.Client(ctx)
	if err != nil {
		t.Fatal("daemon start", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if current, err := OpenSessionClient(dir); err == nil {
			current.Retire(ctx)
		}
	})
	if _, err = c.Use(ctx); !errors.Is(err, ErrLocked) {
		t.Fatal("locked daemon", err)
	}
	if err = c.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	other, err := OpenSessionClient(dir)
	if err != nil {
		t.Fatal(err)
	}
	key, err := other.Use(ctx)
	if err != nil || !sameMaterial(key, want) {
		t.Fatal("shared key session", err)
	}
	key.Destroy()
	stale := *other
	stale.Endpoint.SessionID = "1123456789abcdef0123456789abcdef"
	if _, err = stale.Use(ctx); !errors.Is(err, ErrSessionProtocol) {
		t.Fatal("stale session accepted", err)
	}
	if err = other.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if err = other.Retire(ctx); err != nil {
		t.Fatal(err)
	}
	// Wait until old endpoint disconnects; stale metadata is not readiness.
	for {
		if _, err = other.Status(ctx); err != nil {
			break
		}
		if err = waitSession(ctx); err != nil {
			t.Fatal(err)
		}
	}
	restarted, err := manager.Client(ctx)
	if err != nil {
		t.Fatal("restart", err)
	}
	status, err := restarted.Status(ctx)
	if err != nil || status.State != LockedManual {
		t.Fatal("restart lost latch", err)
	}
	if _, err = restarted.Use(ctx); !errors.Is(err, ErrLocked) {
		t.Fatal("manual lock bypass", err)
	}
	if err = restarted.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	if err = restarted.Retire(ctx); err != nil {
		t.Fatal(err)
	}
	keyBytes := want.CopyBytes()
	defer clear(keyBytes)
	for _, name := range []string{keyFilename, "manual-lock.json", "session.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		pass := p.Passphrase.CopyBytes()
		if bytes.Contains(b, keyBytes) || bytes.Contains(b, pass) {
			clear(pass)
			t.Fatal("sensitive session persistence")
		}
		clear(pass)
	}
}
func TestSessionFrameAndEnvironmentBoundary(t *testing.T) {
	for _, body := range [][]byte{[]byte(`{"operation":"use","operation":"lock"}`), []byte(`{"operation":"status","unknown":true}`)} {
		var frame bytes.Buffer
		length := len(body)
		frame.Write([]byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)})
		frame.Write(body)
		var request sessionRequest
		if readSessionFrame(&frame, &request) == nil {
			t.Fatal("ambiguous frame accepted")
		}
	}
	for _, header := range [][]byte{{0, 0, 0, 0}, {0x7f, 0xff, 0xff, 0xff}} {
		var req sessionRequest
		if !errors.Is(readSessionFrame(bytes.NewReader(header), &req), ErrTooLarge) {
			t.Fatal("frame size accepted")
		}
	}
	env := sessionDaemonEnvironment([]string{"PATH=/synthetic/bin", "DEVTOOLS_TASK_CONTEXT=synthetic-context", "API_TOKEN=synthetic-token", "SystemRoot=C:\\Windows"})
	if len(env) != 2 {
		t.Fatal("daemon inherited credentials")
	}
	if _, err := json.Marshal(NewMaterial([]byte("synthetic"))); err == nil {
		t.Fatal("material diagnostic serialization")
	}
}

func TestSessionAbruptDaemonCrashRetainsManualLock(t *testing.T) {
	dir, p, _ := sessionFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "__vault-session", dir)
	cmd.Env = sessionDaemonEnvironment(os.Environ())
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var c *SessionClient
	for {
		c, err = OpenSessionClient(dir)
		if err == nil {
			if _, err = c.Status(ctx); err == nil {
				break
			}
		}
		if err = waitSession(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = c.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	if err = c.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	waited = true
	restarted, err := (SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Retire(context.Background())
	status, err := restarted.Status(ctx)
	if err != nil || status.State != LockedManual {
		t.Fatal("crash lost manual intent", err)
	}
	if _, err = restarted.Use(ctx); !errors.Is(err, ErrLocked) {
		t.Fatal("crash auto unlocked", err)
	}
}

func TestSessionProtocolRetirementUsesControlOnly(t *testing.T) {
	dir, p, _ := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	release, acquired, err := maintenance.TryLockExclusive(ctx, filepath.Join(dir, "session-daemon.lock"))
	if err != nil || !acquired {
		t.Fatal(err)
	}
	id, _ := NewID()
	e := SessionEndpoint{Protocol: SessionProtocol + 1, VaultID: p.VaultID, SessionID: id, Endpoint: sessionEndpoint(dir, id)}
	l, err := listenSession(dir, id)
	if err != nil {
		release()
		t.Fatal(err)
	}
	r, err := platformfs.OpenRoot(dir, false)
	if err != nil {
		l.Close()
		release()
		t.Fatal(err)
	}
	b, _ := json.Marshal(e)
	err = r.WriteFile(ctx, "session.json", b, true)
	r.Close()
	if err != nil {
		l.Close()
		release()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		defer release()
		defer l.Close()
		c, err := l.Accept(ctx)
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		s := NewKeySession(dir)
		defer s.Close()
		retired := false
		serveSessionConnection(ctx, c, e, s, &sessionAdmissions{}, func() { retired = true })
		if !retired {
			done <- ErrSessionProtocol
			return
		}
		done <- nil
	}()
	c, err := (SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal("protocol replacement", err)
	}
	defer c.Retire(context.Background())
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c.Endpoint.Protocol != SessionProtocol || c.Endpoint.SessionID == id {
		t.Fatal("old daemon retained")
	}
	if _, err = c.Use(ctx); !errors.Is(err, ErrLocked) {
		t.Fatal("incompatible cache reused", err)
	}
}
