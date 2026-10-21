package valuehandoff

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

type Store struct {
	Root, Profile string
	deliveryHook  func(string)
}
type access struct {
	master   *vault.Material
	client   *vault.SessionClient
	vaultID  string
	identity [32]byte
	ticket   *vault.LaunchTicket
}

func (s Store) access(ctx context.Context, active bool) (*access, error) {
	if !project.ValidProfile(s.Profile) || !filepath.IsAbs(s.Root) {
		return nil, vault.ErrInvalid
	}
	dir := filepath.Join(s.Root, "vault")
	ks := vault.KeyFileStore{Directory: dir}
	k, err := ks.Read()
	if err != nil {
		return nil, err
	}
	identity, err := ks.WriteIdentity()
	if err != nil {
		return nil, err
	}
	var client *vault.SessionClient
	if active {
		client, err = (vault.SessionManager{Directory: dir}).Client(ctx)
	} else {
		client, err = vault.OpenSessionClient(dir)
	}
	if err != nil {
		return nil, err
	}
	a := &access{client: client, vaultID: k.VaultID, identity: identity}
	if active {
		a.master, a.ticket, err = client.UseWithTicket(ctx)
	} else {
		a.master, err = client.InspectCached(ctx)
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}
func (s Store) path(domain, id string) string {
	if domain == "value-runners" || domain == "value-runner-requests" {
		return domain + "/" + id + ".json"
	}
	return domain + "/" + profilekey.Key(s.Profile) + "/" + id + ".json"
}
func scope(id string) string { return strings.ReplaceAll(id, "-", "") }

type bound struct {
	Version int             `json:"version"`
	Root    string          `json:"root"`
	Path    string          `json:"path"`
	Profile string          `json:"profile"`
	Data    json.RawMessage `json:"data"`
}

func (s Store) encode(ctx context.Context, a *access, path, id, purpose string, value any) ([]byte, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	plain, err := json.Marshal(bound{1, filepath.Clean(s.Root), path, s.Profile, b})
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	m := vault.NewMaterial(plain)
	defer m.Destroy()
	return vault.SealProtected(ctx, a.master, a.vaultID, scope(id), purpose, m)
}
func (s Store) decode(ctx context.Context, a *access, path, id, purpose string, out any) error {
	raw, err := maintenance.Read(filepath.Join(s.Root, filepath.FromSlash(path)), vault.MaxDocumentBytes)
	if err != nil {
		return err
	}
	defer clear(raw)
	m, err := vault.OpenProtected(ctx, a.master, a.vaultID, scope(id), purpose, raw)
	if err != nil {
		return err
	}
	defer m.Destroy()
	b := m.CopyBytes()
	defer clear(b)
	var v bound
	if vault.StrictDecode(b, &v) != nil || v.Version != 1 || v.Root != filepath.Clean(s.Root) || v.Path != path || v.Profile != s.Profile {
		return vault.ErrIntegrity
	}
	defer clear(v.Data)
	return vault.StrictDecode(v.Data, out)
}

// Final publication never performs session IPC under key-slot/maintenance gates.
func (s Store) lockCommit(ctx context.Context, a *access) (func(), error) {
	slot, err := maintenance.LockShared(ctx, filepath.Join(s.Root, "vault", "key-slot-write.lock"))
	if err != nil {
		return nil, err
	}
	gate, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		slot()
		return nil, err
	}
	release := func() { gate(); slot() }
	identity, err := (vault.KeyFileStore{Directory: filepath.Join(s.Root, "vault")}).WriteIdentity()
	if err != nil {
		release()
		return nil, err
	}
	if identity != a.identity {
		release()
		return nil, vault.ErrInputChanged
	}
	return release, nil
}
func fingerprint(a *access, action, id string, input any) (string, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	defer clear(b)
	k := a.master.CopyBytes()
	defer clear(k)
	h := hmac.New(sha256.New, k)
	h.Write([]byte("value-execution-request-v1\x00"))
	h.Write([]byte(action))
	h.Write([]byte{0})
	h.Write([]byte(id))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s Store) replay(ctx context.Context, a *access, id, fp string) (Result, bool, *protocol.Error) {
	var r receipt
	err := s.decode(ctx, a, s.path("value-execution-requests", id), id, vault.MaterialExecutionRequest, &r)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, values.VaultError(ctx, err)
	}
	if !time.Now().Before(r.ExpiresAt) {
		return Result{}, true, failure("request_expired")
	}
	if !hmac.Equal([]byte(r.Fingerprint), []byte(fp)) {
		return Result{}, true, failure("request_conflict")
	}
	if r.Pending {
		return r.Result, true, failure("delivery_outcome_unknown")
	}
	if r.ErrorCode != "" {
		if r.ErrorCode != "delivery_outcome_unknown" {
			return Result{}, true, failure(vault.ErrIntegrity.Error())
		}
		return r.Result, true, failure(r.ErrorCode)
	}
	r.Result.Replayed = true
	return r.Result, true, nil
}
func (s Store) requestFile(ctx context.Context, a *access, id, fp, action string, result Result, pending bool) ([]byte, error) {
	now := time.Now().UTC()
	return s.encode(ctx, a, s.path("value-execution-requests", id), id, vault.MaterialExecutionRequest, receipt{Action: action, Fingerprint: fp, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), Pending: pending, Result: result})
}
func (s Store) readRecord(ctx context.Context, a *access, id string) (record, error) {
	var r record
	err := s.decode(ctx, a, s.path("value-executions", id), id, vault.MaterialExecutionRecord, &r)
	if err == nil && (r.Item.ID != id || !idRE.MatchString(r.Item.InputID) || r.Item.Revision == 0 || r.Item.ExpiresAt.IsZero() || r.Ticket.Profile != s.Profile) {
		err = vault.ErrIntegrity
	}
	return r, err
}
func (s Store) recordFiles(ctx context.Context, a *access, r record, requestID, fp, action string, result Result, pending bool) (map[string][]byte, error) {
	path := s.path("value-executions", r.Item.ID)
	b, err := s.encode(ctx, a, path, r.Item.ID, vault.MaterialExecutionRecord, r)
	if err != nil {
		return nil, err
	}
	rb, err := s.requestFile(ctx, a, requestID, fp, action, result, pending)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{path: b, s.path("value-execution-requests", requestID): rb}, nil
}
func (s Store) operationLease(ctx context.Context, id string) (func(), error) {
	return maintenance.LockExclusive(ctx, filepath.Join(s.Root, "value-execution-locks", profilekey.Key(s.Profile), id+".lock"))
}
