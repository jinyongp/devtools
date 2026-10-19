package vault

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const secretService = "org.freedesktop.secrets"
const secretRoot = dbus.ObjectPath("/org/freedesktop/secrets")
const secretInterface = "org.freedesktop.Secret."

type SecretServiceProtector struct {
	VaultID          string
	AllowInteraction bool
	// Empty selects the existing user bus, never autolaunches another service.
	BusAddress string
}

func (p SecretServiceProtector) Identity() ProtectionIdentity {
	return ProtectionIdentity{Backend: "secret-service", VaultID: p.VaultID}
}

type serviceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}
type secretConnection struct {
	conn        *dbus.Conn
	owner       string
	session     dbus.ObjectPath
	interactive bool
}

// Only a single, local user bus is accepted. In particular, DBUS env overrides
// cannot route a root key to TCP, exec, autolaunch or a remote fallback address.
func userBusSocket(address string) (string, error) {
	if address == "" {
		return "/run/user/" + strconv.Itoa(os.Getuid()) + "/bus", nil
	}
	if !strings.HasPrefix(address, "unix:") || strings.Contains(address, ";") {
		return "", ErrUnavailable
	}
	var socket string
	seen := map[string]bool{}
	for _, part := range strings.Split(strings.TrimPrefix(address, "unix:"), ",") {
		key, value, ok := strings.Cut(part, "=")
		if !ok || seen[key] {
			return "", ErrUnavailable
		}
		seen[key] = true
		value, err := url.PathUnescape(value)
		if err != nil || strings.ContainsRune(value, 0) {
			return "", ErrUnavailable
		}
		switch key {
		case "path":
			if socket != "" || !strings.HasPrefix(value, "/") {
				return "", ErrUnavailable
			}
			socket = value
		case "abstract":
			if socket != "" || value == "" {
				return "", ErrUnavailable
			}
			socket = "\x00" + value
		case "guid": // Authentication still pins the Unix peer and service UID.
		default:
			return "", ErrUnavailable
		}
	}
	if socket == "" {
		return "", ErrUnavailable
	}
	return socket, nil
}

func dialUserBus(ctx context.Context, address string) (*dbus.Conn, error) {
	socket, err := userBusSocket(address)
	if err != nil {
		return nil, err
	}
	raw, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, secretError(ctx, err)
	}
	keep := false
	defer func() {
		if !keep {
			raw.Close()
		}
	}()
	u := raw.(*net.UnixConn)
	sys, err := u.SyscallConn()
	if err != nil {
		return nil, ErrUnavailable
	}
	var cred *unix.Ucred
	var credErr error
	if err = sys.Control(func(fd uintptr) { cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil || credErr != nil || cred == nil || cred.Uid != uint32(os.Getuid()) {
		return nil, ErrDenied
	}
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = u.SetDeadline(deadline); err != nil {
		return nil, ErrUnavailable
	}
	c, err := dbus.NewConn(raw, dbus.WithContext(ctx))
	if err != nil {
		return nil, ErrUnavailable
	}
	if err = c.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Getuid()))}); err == nil {
		err = c.Hello()
	}
	if err != nil {
		c.Close()
		return nil, secretError(ctx, err)
	}
	if err = u.SetDeadline(time.Time{}); err != nil {
		c.Close()
		return nil, ErrUnavailable
	}
	keep = true
	return c, nil
}

func secretError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var e dbus.Error
	var pointer *dbus.Error
	matched := errors.As(err, &e)
	if !matched && errors.As(err, &pointer) && pointer != nil {
		e = *pointer
		matched = true
	}
	if matched {
		switch e.Name {
		case secretInterface + "Error.IsLocked":
			return ErrBackendLocked
		case secretInterface + "Error.NoSuchObject", "org.freedesktop.DBus.Error.UnknownObject":
			return ErrKeyMissing
		case "org.freedesktop.DBus.Error.AccessDenied", "org.freedesktop.DBus.Error.AuthFailed":
			return ErrDenied
		case secretInterface + "Error.NoSession":
			return ErrRecovery
		}
	}
	return ErrUnavailable
}

func (p SecretServiceProtector) connect(ctx context.Context) (*secretConnection, error) {
	if !validID(p.VaultID) {
		return nil, ErrInvalid
	}
	address := p.BusAddress
	if address == "" {
		address = os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	}
	c, err := dialUserBus(ctx, address)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			c.Close()
		}
	}()
	var owner string
	if err = c.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", dbus.FlagNoAutoStart, secretService).Store(&owner); err != nil {
		return nil, secretError(ctx, err)
	}
	var uid uint32
	if err = c.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixUser", 0, owner).Store(&uid); err != nil {
		return nil, secretError(ctx, err)
	}
	if uid != uint32(os.Getuid()) || !strings.HasPrefix(owner, ":") {
		return nil, ErrDenied
	}
	s := &secretConnection{conn: c, owner: owner, interactive: p.AllowInteraction}
	var output dbus.Variant
	if err = s.call(ctx, secretRoot, "Service.OpenSession", "plain", dbus.MakeVariant("")).Store(&output, &s.session); err != nil {
		return nil, secretError(ctx, err)
	}
	if !s.session.IsValid() || s.session == "/" || output.Value() != "" {
		return nil, ErrIntegrity
	}
	keep = true
	return s, nil
}
func (s *secretConnection) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	return s.conn.Object(s.owner, path).CallWithContext(ctx, secretInterface+method, dbus.FlagNoAutoStart, args...)
}
func (p SecretServiceProtector) attributes() map[string]string {
	return map[string]string{"application": "devtools", "namespace": "vault-v1", "vault-id": p.VaultID}
}
func (s *secretConnection) find(ctx context.Context, p SecretServiceProtector) (dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := s.call(ctx, secretRoot, "Service.SearchItems", p.attributes()).Store(&unlocked, &locked); err != nil {
		return "", secretError(ctx, err)
	}
	if len(unlocked)+len(locked) == 0 {
		return "", ErrKeyMissing
	}
	if len(unlocked)+len(locked) != 1 {
		return "", ErrRecovery
	}
	if len(locked) == 1 {
		if err := s.unlock(ctx, locked[0]); err != nil {
			return "", err
		}
		return locked[0], nil
	}
	if !unlocked[0].IsValid() || unlocked[0] == "/" {
		return "", ErrIntegrity
	}
	return unlocked[0], nil
}
func (s *secretConnection) prompt(ctx context.Context, path dbus.ObjectPath) error {
	if path == "/" {
		return nil
	}
	if !path.IsValid() {
		return ErrIntegrity
	}
	if !s.interactive {
		return ErrInteractionRequired
	}
	opts := []dbus.MatchOption{dbus.WithMatchSender(s.owner), dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(secretInterface + "Prompt"), dbus.WithMatchMember("Completed")}
	if err := s.conn.AddMatchSignalContext(ctx, opts...); err != nil {
		return secretError(ctx, err)
	}
	defer s.conn.RemoveMatchSignal(opts...)
	signals := make(chan *dbus.Signal, 4)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)
	if err := s.call(ctx, path, "Prompt.Prompt", "").Err; err != nil {
		return secretError(ctx, err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case signal, ok := <-signals:
			if !ok {
				return ErrUnavailable
			}
			if signal.Sender != s.owner || signal.Path != path || signal.Name != secretInterface+"Prompt.Completed" {
				continue
			}
			if len(signal.Body) != 2 {
				return ErrIntegrity
			}
			dismissed, ok := signal.Body[0].(bool)
			if !ok {
				return ErrIntegrity
			}
			if dismissed {
				return ErrDenied
			}
			return nil
		}
	}
}
func (s *secretConnection) unlock(ctx context.Context, path dbus.ObjectPath) error {
	if !path.IsValid() || path == "/" {
		return ErrIntegrity
	}
	if !s.interactive {
		return ErrBackendLocked
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := s.call(ctx, secretRoot, "Service.Unlock", []dbus.ObjectPath{path}).Store(&unlocked, &prompt); err != nil {
		return secretError(ctx, err)
	}
	if prompt == "/" {
		for _, item := range unlocked {
			if item == path {
				return nil
			}
		}
		return ErrBackendLocked
	}
	return s.prompt(ctx, prompt)
}

func (p SecretServiceProtector) Wrap(ctx context.Context, key []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	b, err := osKeyPayload("secret-service", p.VaultID, key)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	s, err := p.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer s.conn.Close()
	if _, err = s.find(ctx, p); !errors.Is(err, ErrKeyMissing) {
		if err == nil {
			err = ErrRecovery
		}
		return nil, err
	}
	var collection dbus.ObjectPath
	if err = s.call(ctx, secretRoot, "Service.ReadAlias", "default").Store(&collection); err != nil {
		return nil, secretError(ctx, err)
	}
	if !collection.IsValid() || collection == "/" {
		return nil, ErrUnavailable
	}
	var locked dbus.Variant
	if err = s.conn.Object(s.owner, collection).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart, secretInterface+"Collection", "Locked").Store(&locked); err != nil {
		return nil, secretError(ctx, err)
	}
	lockedValue, ok := locked.Value().(bool)
	if !ok {
		return nil, ErrIntegrity
	}
	if lockedValue {
		if err = s.unlock(ctx, collection); err != nil {
			return nil, err
		}
	}
	props := map[string]dbus.Variant{secretInterface + "Item.Label": dbus.MakeVariant("devtools vault key"), secretInterface + "Item.Attributes": dbus.MakeVariant(p.attributes())}
	var item, prompt dbus.ObjectPath
	secret := serviceSecret{Session: s.session, Parameters: []byte{}, Value: b, ContentType: "application/octet-stream"}
	if err = s.call(ctx, collection, "Collection.CreateItem", props, secret, false).Store(&item, &prompt); err != nil {
		return nil, secretError(ctx, err)
	}
	if err = s.prompt(ctx, prompt); err != nil {
		return nil, err
	}
	if _, err = s.find(ctx, p); err != nil {
		return nil, err
	}
	return []byte("secret-service/v1/" + p.VaultID), nil
}
func (p SecretServiceProtector) Unwrap(ctx context.Context, wrapped []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !bytes.Equal(wrapped, []byte("secret-service/v1/"+p.VaultID)) {
		return nil, ErrInvalid
	}
	s, err := p.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer s.conn.Close()
	item, err := s.find(ctx, p)
	if err != nil {
		return nil, err
	}
	var secret serviceSecret
	if err = s.call(ctx, item, "Item.GetSecret", s.session).Store(&secret); err != nil {
		return nil, secretError(ctx, err)
	}
	defer clear(secret.Value)
	// GNOME Keyring returns text/plain even for binary values (its wire encoder
	// uses that fixed MIME label). Treat the bytes as our typed binary envelope
	// in either case; never decode text, trim NULs or skip payload verification.
	validType := secret.ContentType == "application/octet-stream" || secret.ContentType == "text/plain"
	if secret.Session != s.session || len(secret.Parameters) != 0 || !validType || len(secret.Value) > MaxKeySlotBytes {
		return nil, ErrIntegrity
	}
	return osKeyFromPayload("secret-service", p.VaultID, secret.Value)
}
func (p SecretServiceProtector) Remove(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer s.conn.Close()
	item, err := s.find(ctx, p)
	if err != nil {
		return err
	}
	var prompt dbus.ObjectPath
	if err = s.call(ctx, item, "Item.Delete").Store(&prompt); err != nil {
		return secretError(ctx, err)
	}
	return s.prompt(ctx, prompt)
}
