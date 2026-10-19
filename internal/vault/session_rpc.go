package vault

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

var ErrSessionProtocol = errors.New("key_session_protocol_mismatch")

type SessionEndpoint struct {
	Protocol  int    `json:"protocol"`
	VaultID   string `json:"vault_id"`
	SessionID string `json:"session_id"`
	Endpoint  string `json:"endpoint"`
}
type sessionHello struct {
	SessionEndpoint
	ControlProtocol int    `json:"control_protocol"`
	LockEpoch       uint64 `json:"lock_epoch"`
	Challenge       string `json:"challenge"`
}
type sessionRequest struct {
	LockEpoch   uint64 `json:"lock_epoch"`
	Protocol    int    `json:"protocol"`
	VaultID     string `json:"vault_id"`
	SessionID   string `json:"session_id"`
	Challenge   string `json:"challenge"`
	Operation   string `json:"operation"`
	Passphrase  []byte `json:"passphrase,omitempty"`
	Interactive bool   `json:"interactive,omitempty"`
}

// This is a private wire response. Never forward it into protocol diagnostics.
type sessionResponse struct {
	Status SessionStatus `json:"status"`
	Key    []byte        `json:"key,omitempty"`
	Error  string        `json:"error,omitempty"`
}

type sessionAdmissions struct {
	gate    sync.RWMutex
	epoch   atomic.Uint64
	pending atomic.Int32
}

func sessionCode(err error) string {
	if err == nil {
		return ""
	}
	for _, known := range []error{ErrLocked, ErrKeyMissing, ErrIntegrity, ErrInvalid, ErrTooLarge, ErrUnavailable, ErrRecovery, ErrLockPending, ErrDenied, ErrInteractionRequired, ErrBackendLocked, ErrSessionProtocol} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return ErrUnavailable.Error()
}
func sessionError(code string) error {
	if code == "" {
		return nil
	}
	for _, known := range []error{ErrLocked, ErrKeyMissing, ErrIntegrity, ErrInvalid, ErrTooLarge, ErrUnavailable, ErrRecovery, ErrLockPending, ErrDenied, ErrInteractionRequired, ErrBackendLocked, ErrSessionProtocol} {
		if code == known.Error() {
			return known
		}
	}
	return ErrUnavailable
}
func ReadSessionEndpoint(directory string) (*SessionEndpoint, error) {
	r, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer r.Close()
	b, err := r.ReadFile("session.json", MaxKeySlotBytes)
	if err != nil {
		return nil, ErrUnavailable
	}
	var e SessionEndpoint
	if StrictDecode(b, &e) != nil || !validID(e.VaultID) || !validID(e.SessionID) || e.Protocol < 1 || e.Endpoint != sessionEndpoint(directory, e.SessionID) {
		return nil, ErrRecovery
	}
	return &e, nil
}

// ServeKeySession is the private same-binary daemon entrypoint. It neither loads
// project commands nor starts application processes. A daemon lease prevents
// competing servers from replacing live endpoint metadata.
func ServeKeySession(ctx context.Context, directory string) error {
	r, err := platformfs.OpenRoot(directory, false)
	if err != nil {
		return ErrRecovery
	}
	defer r.Close()
	release, acquired, err := maintenance.TryLockExclusive(ctx, filepath.Join(directory, "session-daemon.lock"))
	if err != nil {
		return ErrRecovery
	}
	if !acquired {
		return ErrUnavailable
	}
	defer release()
	k, err := (KeyFileStore{Directory: directory}).Read()
	if err != nil {
		return err
	}
	id, err := NewID()
	if err != nil {
		return ErrUnavailable
	}
	endpoint := SessionEndpoint{Protocol: SessionProtocol, VaultID: k.VaultID, SessionID: id, Endpoint: sessionEndpoint(directory, id)}
	l, err := listenSession(directory, id)
	if err != nil {
		return err
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	b, err := json.Marshal(endpoint)
	if err != nil {
		return ErrInvalid
	}
	if err = r.WriteFile(ctx, "session.json", b, true); err != nil {
		return ErrRecovery
	}
	s := NewKeySession(directory)
	defer s.Close()
	admissions := &sessionAdmissions{}
	slots := make(chan struct{}, maxSessionConnections)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	for {
		c, err := l.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return ErrUnavailable
		}
		select {
		case slots <- struct{}{}:
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-slots }()
				defer c.Close()
				serveSessionConnection(ctx, c, endpoint, s, admissions, cancel)
			}()
		default:
			c.Close()
		}
	}
}
func serveSessionConnection(ctx context.Context, c sessionConnection, endpoint SessionEndpoint, s *KeySession, admissions *sessionAdmissions, retire context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := c.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return
	}
	challenge, err := NewID()
	if err != nil {
		return
	}
	if writeSessionFrame(c, sessionHello{SessionEndpoint: endpoint, ControlProtocol: 1, LockEpoch: admissions.epoch.Load(), Challenge: challenge}) != nil {
		return
	}
	var req sessionRequest
	defer func() { clear(req.Passphrase) }()
	if readSessionFrame(c, &req) != nil {
		return
	}
	if req.Protocol != endpoint.Protocol || req.VaultID != endpoint.VaultID || req.SessionID != endpoint.SessionID || subtle.ConstantTimeCompare([]byte(req.Challenge), []byte(challenge)) != 1 {
		writeSessionFrame(c, sessionResponse{Error: ErrSessionProtocol.Error()})
		return
	}
	var response sessionResponse
	if req.Operation != "unlock" && (len(req.Passphrase) > 0 || req.Interactive) {
		writeSessionFrame(c, sessionResponse{Status: s.Status(), Error: ErrInvalid.Error()})
		return
	}
	// Hold a read admission until the entire key response is written. A lock
	// closes new admissions and drains those writes before acknowledging.
	switch req.Operation {
	case "use":
		admissions.gate.RLock()
		defer admissions.gate.RUnlock()
		if admissions.pending.Load() > 0 {
			err = ErrLockPending
		}
	case "unlock":
		admissions.gate.Lock()
		defer admissions.gate.Unlock()
		if admissions.pending.Load() > 0 || req.LockEpoch != admissions.epoch.Load() {
			err = ErrLockPending
		}
	case "lock", "retire":
		admissions.pending.Add(1)
		admissions.epoch.Add(1)
		defer admissions.pending.Add(-1)
		if req.Operation == "lock" {
			// Persist manual intent before waiting for an already admitted
			// response. A daemon crash while draining must not lose the latch.
			err = s.Lock(ctx)
		}
		admissions.gate.Lock()
		defer admissions.gate.Unlock()
	}
	if err != nil {
		writeSessionFrame(c, sessionResponse{Status: s.Status(), Error: sessionCode(err)})
		return
	}
	switch req.Operation {
	case "status":
	case "unlock":
		if len(req.Passphrase) > MaxPassphraseBytes {
			err = ErrTooLarge
			break
		}
		pass := NewMaterial(req.Passphrase)
		err = s.Unlock(ctx, pass, req.Interactive)
		pass.Destroy()
	case "use":
		var key *Material
		key, err = s.Use(ctx)
		if err == nil {
			response.Key = key.CopyBytes()
			key.Destroy()
			defer clear(response.Key)
		}
	case "lock":
		// Intent and cache lock were committed before draining admissions.
	case "retire":
		// Retire clears only volatile state. A durable manual latch survives.
		s.Close()
	default:
		err = ErrInvalid
	}
	response.Status = s.Status()
	response.Error = sessionCode(err)
	if writeSessionFrame(c, response) == nil && req.Operation == "retire" && err == nil {
		retire()
	}
}

// SessionClient only connects to private, vault-bound endpoint metadata. Both
// endpoints verify OS users before the protocol exchanges any key material.
type SessionClient struct {
	Directory string
	Endpoint  SessionEndpoint
}

func OpenSessionClient(directory string) (*SessionClient, error) {
	e, err := ReadSessionEndpoint(directory)
	if err != nil {
		return nil, err
	}
	if e.Protocol != SessionProtocol {
		return nil, ErrSessionProtocol
	}
	k, err := (KeyFileStore{Directory: directory}).Read()
	if err != nil {
		return nil, err
	}
	if k.VaultID != e.VaultID {
		return nil, ErrRecovery
	}
	return &SessionClient{Directory: directory, Endpoint: *e}, nil
}
func (c *SessionClient) request(ctx context.Context, operation string, passphrase []byte, interactive bool) (*sessionResponse, error) {
	if c.Endpoint.Protocol != SessionProtocol && operation != "retire" {
		return nil, ErrSessionProtocol
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := dialSession(ctx, c.Endpoint.Endpoint)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = connection.SetDeadline(deadline); err != nil {
		return nil, ErrUnavailable
	}
	var hello sessionHello
	if err = readSessionFrame(connection, &hello); err != nil {
		return nil, ErrUnavailable
	}
	if hello.SessionEndpoint != c.Endpoint || hello.ControlProtocol != 1 || !validID(hello.Challenge) {
		return nil, ErrSessionProtocol
	}
	req := sessionRequest{Protocol: c.Endpoint.Protocol, LockEpoch: hello.LockEpoch, VaultID: c.Endpoint.VaultID, SessionID: c.Endpoint.SessionID, Challenge: hello.Challenge, Operation: operation, Passphrase: passphrase, Interactive: interactive}
	if err = writeSessionFrame(connection, req); err != nil {
		return nil, ErrUnavailable
	}
	var response sessionResponse
	if err = readSessionFrame(connection, &response); err != nil {
		clear(response.Key)
		return nil, ErrUnavailable
	}
	if response.Status.VaultID != c.Endpoint.VaultID {
		clear(response.Key)
		return nil, ErrIntegrity
	}
	if err = sessionError(response.Error); err != nil {
		clear(response.Key)
		return nil, err
	}
	if operation != "use" && len(response.Key) > 0 {
		clear(response.Key)
		return nil, ErrIntegrity
	}
	return &response, nil
}
func (c *SessionClient) Status(ctx context.Context) (SessionStatus, error) {
	r, err := c.request(ctx, "status", nil, false)
	if err != nil {
		return SessionStatus{}, err
	}
	return r.Status, nil
}
func (c *SessionClient) Unlock(ctx context.Context, passphrase *Material, interactive bool) error {
	b := passphrase.CopyBytes()
	defer clear(b)
	_, err := c.request(ctx, "unlock", b, interactive)
	return err
}
func (c *SessionClient) Use(ctx context.Context) (*Material, error) {
	r, err := c.request(ctx, "use", nil, false)
	if err != nil {
		return nil, err
	}
	defer clear(r.Key)
	if len(r.Key) != 32 {
		return nil, ErrIntegrity
	}
	return NewMaterial(r.Key), nil
}
func (c *SessionClient) Lock(ctx context.Context) error {
	_, err := c.request(ctx, "lock", nil, false)
	return err
}
func (c *SessionClient) Retire(ctx context.Context) error {
	_, err := c.request(ctx, "retire", nil, false)
	return err
}
