// Package expose owns sharing metadata, in-memory authentication and ingress
// lifetimes. It never stores a password or an unkeyed password digest.
package expose

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Public struct {
	ID          string           `json:"id"`
	Route       network.RouteRef `json:"route_ref"`
	Provider    string           `json:"provider"`
	State       string           `json:"state"`
	Generation  string           `json:"generation"`
	Listener    string           `json:"listener"`
	Host        string           `json:"host"`
	URL         string           `json:"url"`
	Auth        bool             `json:"auth"`
	OriginPort  int              `json:"origin_port"`
	ServePort   int              `json:"serve_port"`
	Reason      string           `json:"reason"`
	CreatedAt   time.Time        `json:"created_at"`
	ClientSetup []string         `json:"client_setup"`
}

type Record struct {
	Public
	Pin           network.Pin `json:"pin"`
	Identity      string      `json:"identity"`
	Reservation   string      `json:"reservation"`
	Daemon        string      `json:"daemon"`
	ProviderOwner string      `json:"provider_owner,omitempty"`
}
type Store struct{ Data string }

func Failure(code string) *protocol.Error {
	return protocol.NewError(code, "Exposure operation could not satisfy the requested condition.", 3, nil)
}
func validID(id string) bool          { return (network.RouteRef{Scope: "global", RouteID: id}).Valid() }
func (s Store) Path(id string) string { return filepath.Join(s.Data, "exposures", id+".json") }
func (s Store) Read(id string) (Record, *protocol.Error) {
	if !validID(id) {
		return Record{}, Failure("invalid_argument")
	}
	var r Record
	if e := tasks.ReadPrivate(s.Path(id), &r); e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return r, Failure("exposure_not_found")
		}
		return r, Failure("invalid_storage")
	}
	if r.ID != id || !r.Pin.Valid() || r.Route != r.Pin.Route || !validID(r.Generation) {
		return Record{}, Failure("invalid_storage")
	}
	return r, nil
}
func (s Store) Write(r Record) *protocol.Error {
	if !validID(r.ID) || !r.Pin.Valid() {
		return Failure("invalid_storage")
	}
	if tasks.PrivateDir(filepath.Dir(s.Path(r.ID))) != nil || tasks.WritePrivate(s.Path(r.ID), r) != nil {
		return Failure("io_error")
	}
	return nil
}
func (s Store) List() ([]Record, *protocol.Error) {
	items := []Record{}
	entries, e := os.ReadDir(filepath.Join(s.Data, "exposures"))
	if errors.Is(e, os.ErrNotExist) {
		return items, nil
	}
	if e != nil {
		return nil, Failure("io_error")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validID(id) {
			continue
		}
		r, e := s.Read(id)
		if e != nil {
			return nil, e
		}
		items = append(items, r)
	}
	return items, nil
}

// Commitment is host-local and keyed, including operation identity and auth
// mode. A missing key with existing operations is recovery, never key rotation.
func (s Store) Commitment(ctx context.Context, id, auth string, noAuth bool) (string, *protocol.Error) {
	if !validID(id) {
		return "", Failure("invalid_argument")
	}
	root := filepath.Join(s.Data, "exposure-auth")
	if tasks.PrivateDir(root) != nil {
		return "", Failure("io_error")
	}
	unlock, e := tasks.Lock(ctx, filepath.Join(root, "key.lock"))
	if e != nil {
		return "", Failure("canceled")
	}
	defer unlock()
	path := filepath.Join(root, "key.json")
	var key []byte
	e = tasks.ReadPrivate(path, &key)
	if errors.Is(e, os.ErrNotExist) {
		if _, e := os.Lstat(filepath.Join(root, "initialized.json")); !errors.Is(e, os.ErrNotExist) {
			return "", Failure("auth_commitment_key_missing")
		}
		entries, _ := os.ReadDir(filepath.Join(s.Data, "exposures"))
		if len(entries) > 0 {
			return "", Failure("auth_commitment_key_missing")
		}
		key = make([]byte, 32)
		if _, e = rand.Read(key); e != nil {
			return "", Failure("io_error")
		}
		if tasks.WritePrivate(path, key) != nil {
			return "", Failure("io_error")
		}
		if tasks.WritePrivate(filepath.Join(root, "initialized.json"), true) != nil {
			return "", Failure("io_error")
		}
	} else if e != nil || len(key) != 32 {
		return "", Failure("auth_commitment_key_invalid")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id))
	mac.Write([]byte{0})
	if noAuth {
		mac.Write([]byte("no-auth"))
	} else {
		mac.Write([]byte("basic"))
	}
	mac.Write([]byte{0})
	mac.Write([]byte(auth))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func ValidateAuth(auth string, noAuth bool) *protocol.Error {
	if noAuth && auth != "" {
		return Failure("invalid_argument")
	}
	if auth == "" {
		if noAuth {
			return nil
		}
		return Failure("authentication_required")
	}
	user, password, ok := strings.Cut(auth, ":")
	if !ok || user == "" || password == "" || len(auth) > 8192 || strings.ContainsAny(auth, "\r\n\x00") {
		return Failure("invalid_authentication")
	}
	return nil
}
