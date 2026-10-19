package vault

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestUserBusAddressRejectsRemoteAndFallback(t *testing.T) {
	for _, s := range []string{"tcp:host=localhost,port=123", "autolaunch:", "unixexec:path=x", "unix:path=/a;tcp:host=x", "unix:path=/a,path=/b", "unix:path=relative", "unix:path=/a,abstract=b", "unix:path=/a%00b", "unix:path=%XX"} {
		if _, err := userBusSocket(s); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("accepted %q", s)
		}
	}
	for s, want := range map[string]string{"unix:path=/tmp/a%20b,guid=abc": "/tmp/a b", "unix:abstract=test": "\x00test"} {
		if got, err := userBusSocket(s); err != nil || got != want {
			t.Fatal(s, err)
		}
	}
}

func privateSecretBus(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("D-Bus integration needs a disposable dbus-daemon")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); cmd.Wait() })
	line := make(chan string, 1)
	go func() { s, _ := bufio.NewReader(out).ReadString('\n'); line <- strings.TrimSpace(s) }()
	select {
	case s := <-line:
		if s == "" {
			t.Fatal("bus did not start")
		}
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("bus start timeout")
	}
	return ""
}

// The fixture implements the real D-Bus contract over a private Unix bus. It
// tests our transport/codec/errors; it is not OS keyring encryption validation.
type fixtureSecretService struct {
	mu          sync.Mutex
	conn        *dbus.Conn
	keys        map[string][]byte
	locked      bool
	fault       string
	contentType string
	pause       chan struct{}
	entered     chan struct{}
}

func (f *fixtureSecretService) OpenSession(algorithm string, input dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	if algorithm != "plain" || input.Value() != "" {
		return dbus.Variant{}, "/", dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", nil)
	}
	return dbus.MakeVariant(""), "/org/freedesktop/secrets/session/test", nil
}
func (f *fixtureSecretService) ReadAlias(alias string) (dbus.ObjectPath, *dbus.Error) {
	return "/org/freedesktop/secrets/collection/test", nil
}
func (f *fixtureSecretService) Unlock(paths []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	return []dbus.ObjectPath{}, "/org/freedesktop/secrets/prompt/test", nil
}

type fixtureSecretPrompt struct{ service *fixtureSecretService }

func (p fixtureSecretPrompt) Prompt(window string) *dbus.Error {
	if err := p.service.conn.Emit("/org/freedesktop/secrets/prompt/test", secretInterface+"Prompt.Completed", true, dbus.MakeVariant([]dbus.ObjectPath{})); err != nil {
		return dbus.NewError("org.freedesktop.DBus.Error.Failed", nil)
	}
	return nil
}
func (f *fixtureSecretService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := []dbus.ObjectPath{}
	if _, ok := f.keys[attrs["vault-id"]]; ok {
		items = append(items, dbus.ObjectPath("/org/freedesktop/secrets/item/"+attrs["vault-id"]))
	}
	if f.locked {
		return []dbus.ObjectPath{}, items, nil
	}
	return items, []dbus.ObjectPath{}, nil
}
func (f *fixtureSecretService) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return dbus.MakeVariant(f.locked), nil
}
func (f *fixtureSecretService) CreateItem(props map[string]dbus.Variant, secret serviceSecret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if replace {
		return "/", "/", dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", nil)
	}
	attrs, ok := props[secretInterface+"Item.Attributes"].Value().(map[string]string)
	if !ok {
		return "/", "/", dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", nil)
	}
	id := attrs["vault-id"]
	f.keys[id] = bytes.Clone(secret.Value)
	path := dbus.ObjectPath("/org/freedesktop/secrets/item/" + id)
	if err := f.conn.Export(fixtureSecretItem{f, id}, path, secretInterface+"Item"); err != nil {
		return "/", "/", dbus.NewError("org.freedesktop.DBus.Error.Failed", nil)
	}
	return path, "/", nil
}

type fixtureSecretItem struct {
	service *fixtureSecretService
	id      string
}

func (i fixtureSecretItem) GetSecret(session dbus.ObjectPath) (serviceSecret, *dbus.Error) {
	i.service.mu.Lock()
	defer i.service.mu.Unlock()
	if i.service.pause != nil {
		close(i.service.entered)
		<-i.service.pause
	}
	if i.service.fault != "" {
		return serviceSecret{}, dbus.NewError(i.service.fault, nil)
	}
	contentType := i.service.contentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return serviceSecret{Session: session, Parameters: []byte{}, Value: bytes.Clone(i.service.keys[i.id]), ContentType: contentType}, nil
}
func (i fixtureSecretItem) Delete() (dbus.ObjectPath, *dbus.Error) {
	i.service.mu.Lock()
	defer i.service.mu.Unlock()
	clear(i.service.keys[i.id])
	delete(i.service.keys, i.id)
	return "/", nil
}
func installFixtureSecretService(t *testing.T, address string) *fixtureSecretService {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, err := dialUserBus(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	f := &fixtureSecretService{conn: c, keys: make(map[string][]byte)}
	if err := c.Export(fixtureSecretPrompt{f}, "/org/freedesktop/secrets/prompt/test", secretInterface+"Prompt"); err != nil {
		t.Fatal(err)
	}
	for _, e := range []struct {
		path  dbus.ObjectPath
		iface string
	}{{secretRoot, secretInterface + "Service"}, {"/org/freedesktop/secrets/collection/test", secretInterface + "Collection"}, {"/org/freedesktop/secrets/collection/test", "org.freedesktop.DBus.Properties"}} {
		if err := c.Export(f, e.path, e.iface); err != nil {
			t.Fatal(err)
		}
	}
	if reply, err := c.RequestName(secretService, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatal("service name", err)
	}
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, v := range f.keys {
			clear(v)
		}
	})
	return f
}

func TestSecretServiceRealPrivateBusContract(t *testing.T) {
	address := privateSecretBus(t)
	p := SecretServiceProtector{VaultID: "0123456789abcdef0123456789abcdef", BusAddress: address}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key := bytes.Repeat([]byte{0x52}, 32)
	if _, err := p.Wrap(ctx, key); !errors.Is(err, ErrUnavailable) {
		t.Fatal("absent service", err)
	}
	f := installFixtureSecretService(t, address)
	wrapped, err := p.Wrap(ctx, key)
	if err != nil {
		t.Fatal("wrap", err)
	}
	if bytes.Contains(wrapped, key) {
		t.Fatal("local slot contains plaintext")
	}
	got, err := p.Unwrap(ctx, wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("roundtrip", err)
	}
	clear(got)
	// GNOME uses a fixed text/plain label for its binary Secret.Value bytes.
	f.mu.Lock()
	f.contentType = "text/plain"
	f.mu.Unlock()
	got, err = p.Unwrap(ctx, wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("GNOME binary payload with text MIME label", err)
	}
	clear(got)
	f.mu.Lock()
	f.contentType = "application/unknown"
	f.mu.Unlock()
	if got, err := p.Unwrap(ctx, wrapped); !errors.Is(err, ErrIntegrity) || len(got) != 0 {
		clear(got)
		t.Fatal("unrecognized MIME label accepted", err)
	}
	f.mu.Lock()
	f.contentType = "text/plain"
	f.mu.Unlock()
	if _, err = p.Wrap(ctx, key); !errors.Is(err, ErrRecovery) {
		t.Fatal("duplicate", err)
	}
	f.mu.Lock()
	f.locked = true
	f.mu.Unlock()
	if _, err = p.Unwrap(ctx, wrapped); !errors.Is(err, ErrBackendLocked) {
		t.Fatal("locked", err)
	}
	p.AllowInteraction = true
	if _, err = p.Unwrap(ctx, wrapped); !errors.Is(err, ErrDenied) {
		t.Fatal("authentication canceled", err)
	}
	p.AllowInteraction = false
	f.mu.Lock()
	f.locked = false
	f.fault = secretInterface + "Error.NoSession"
	f.mu.Unlock()
	if _, err = p.Unwrap(ctx, wrapped); !errors.Is(err, ErrRecovery) {
		t.Fatal("session lost", err)
	}
	f.mu.Lock()
	f.fault = ""
	f.keys[p.VaultID][0] ^= 1
	f.mu.Unlock()
	if _, err = p.Unwrap(ctx, wrapped); !errors.Is(err, ErrIntegrity) {
		t.Fatal("corrupt payload", err)
	}
	if err = p.Remove(ctx); err != nil {
		t.Fatal("cleanup", err)
	}
	if _, err = p.Unwrap(ctx, wrapped); !errors.Is(err, ErrKeyMissing) {
		t.Fatal("missing", err)
	}
	f.conn.Close()
	if _, err = p.Unwrap(ctx, wrapped); !errors.Is(err, ErrUnavailable) {
		t.Fatal("service exit", err)
	}
}

func TestNativeSecretServiceSyntheticVault(t *testing.T) {
	if os.Getenv("DEVTOOLS_TEST_NATIVE_SECRET_SERVICE") != "1" {
		t.Skip("requires disposable real Secret Service environment")
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	p := SecretServiceProtector{VaultID: id}
	key := bytes.Repeat([]byte{0x63}, 32)
	wrapped, err := p.Wrap(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Remove(context.Background()); err != nil {
			t.Error("synthetic key cleanup", err)
		}
	})
	got, err := p.Unwrap(context.Background(), wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("native roundtrip", err)
	}
	clear(got)
}

func TestSessionNativeHelperRealPipeAndCancellation(t *testing.T) {
	address := privateSecretBus(t)
	f := installFixtureSecretService(t, address)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	id, _ := NewID()
	p := SecretServiceProtector{VaultID: id, BusAddress: address}
	dir := filepath.Join(privateTempDir(t), "vault")
	want, err := (KeyFileStore{Directory: dir}).Initialize(context.Background(), id, "secret-service", p)
	if err != nil {
		t.Fatal(err)
	}
	defer want.Destroy()
	s := NewKeySession(dir)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key, err := s.Use(ctx)
	if err != nil || !sameMaterial(key, want) {
		t.Fatal("native helper pipe", err)
	}
	key.Destroy()
	s.Close()
	pause, entered := make(chan struct{}), make(chan struct{})
	defer close(pause)
	f.mu.Lock()
	f.pause = pause
	f.entered = entered
	f.mu.Unlock()
	operation, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() {
		key, err := s.Use(operation)
		if key != nil {
			key.Destroy()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("helper did not reach OS wait", ctx.Err())
	}
	stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("native cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native helper remained blocked")
	}
	if s.Status().State != Locked {
		t.Fatal("failed native operation opened session")
	}
}

func TestNativeSecretServiceLockedCollection(t *testing.T) {
	if os.Getenv("DEVTOOLS_DISPOSABLE_HOST") != "1" || os.Getenv("DEVTOOLS_TEST_NATIVE_SECRET_SERVICE") != "1" || !strings.HasPrefix(filepath.Base(os.Getenv("HOME")), "devtools-native-secret-") {
		t.Skip("requires dedicated native Secret Service fixture")
	}
	unlock := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// gnome-keyring-daemon --unlock starts a daemon; it does not send an
		// unlock request to an already running one. Use GNOME's existing control
		// socket protocol, only for this disposable synthetic fixture.
		directory := os.Getenv("GNOME_KEYRING_CONTROL")
		if directory != filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "keyring") || !filepath.IsAbs(directory) {
			return errors.New("unexpected synthetic control directory")
		}
		connection, err := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(directory, "control"))
		if err != nil {
			return errors.New("cannot connect to synthetic GNOME control")
		}
		defer connection.Close()
		if err := verifySessionPeer(connection.(*net.UnixConn)); err != nil {
			return err
		}
		if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		password := []byte("synthetic-gnome-keyring-test-password")
		defer clear(password)
		packet := make([]byte, 12+len(password))
		defer clear(packet)
		binary.BigEndian.PutUint32(packet, uint32(len(packet)))
		binary.BigEndian.PutUint32(packet[4:], 1) // GKD_CONTROL_OP_UNLOCK
		binary.BigEndian.PutUint32(packet[8:], uint32(len(password)))
		copy(packet[12:], password)
		// Linux GNOME receives the NUL credentials byte and reads SO_PEERCRED.
		if err := writeAll(connection, []byte{0}); err != nil {
			return err
		}
		if err := writeAll(connection, packet); err != nil {
			return err
		}
		var reply [8]byte
		if _, err := io.ReadFull(connection, reply[:]); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(reply[:4]) != 8 || binary.BigEndian.Uint32(reply[4:]) != 0 {
			return errors.New("synthetic GNOME control rejected unlock")
		}
		return nil
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	p := SecretServiceProtector{VaultID: id}
	key := bytes.Repeat([]byte{0x75}, 32)
	wrapped, err := p.Wrap(context.Background(), key)
	if err != nil {
		t.Fatal("native setup", err)
	}
	t.Cleanup(func() {
		if err := unlock(); err != nil {
			t.Error("cannot unlock synthetic collection for cleanup")
			return
		}
		if err := p.Remove(context.Background()); err != nil {
			t.Error("synthetic item cleanup", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := p.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.conn.Close()
	var collection dbus.ObjectPath
	if err = s.call(ctx, secretRoot, "Service.ReadAlias", "default").Store(&collection); err != nil || collection == "/" {
		t.Fatal("native default collection unavailable")
	}
	var locked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err = s.call(ctx, secretRoot, "Service.Lock", []dbus.ObjectPath{collection}).Store(&locked, &prompt); err != nil || prompt != "/" {
		t.Fatal("native collection did not lock synchronously", err)
	}
	var state dbus.Variant
	if err = s.conn.Object(s.owner, collection).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart, secretInterface+"Collection", "Locked").Store(&state); err != nil || state.Value() != true {
		t.Fatal("native collection was not locked", err)
	}
	got, err := p.Unwrap(ctx, wrapped)
	leaked := len(got) > 0
	clear(got)
	if leaked || !errors.Is(err, ErrBackendLocked) {
		t.Fatal("locked native collection allowed silent access", err)
	}
	if err = unlock(); err != nil {
		t.Fatal("native synthetic unlock failed")
	}
	got, err = p.Unwrap(context.Background(), wrapped)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("native unlock did not preserve key", err)
	}
	clear(got)
}

func TestNativeSecretServiceUnavailableDoesNotActivate(t *testing.T) {
	if os.Getenv("DEVTOOLS_TEST_NATIVE_SECRET_SERVICE_ABSENT") != "1" {
		t.Skip("requires stopped native service on a dedicated bus")
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	p := SecretServiceProtector{VaultID: id}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := p.Unwrap(ctx, []byte("secret-service/v1/"+id))
	leaked := len(got) > 0
	clear(got)
	if leaked || !errors.Is(err, ErrUnavailable) {
		t.Fatal("absent service did not fail without fallback", err)
	}
	connection, err := dialUserBus(ctx, os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var hasOwner bool
	if err = connection.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", dbus.FlagNoAutoStart, secretService).Store(&hasOwner); err != nil || hasOwner {
		t.Fatal("absent service was activated", err)
	}
}
