// Package profiletransfer manages the local private identity used for cross-device profile transfer.
package profiletransfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/jinyongp/devtools/internal/archivekey"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

const (
	identityLimit = 4096
)

var (
	ErrIdentityMissing = errors.New("transfer identity missing")
	ErrInvalidIdentity = errors.New("invalid transfer identity")
)

type Store struct {
	Data string
}

type Prepared struct {
	Recipient string
	Changed   bool
}

func (s Store) directory() string    { return filepath.Join(s.Data, "profile-transfer") }
func (s Store) identityPath() string { return filepath.Join(s.directory(), "identity-pq.txt") }
func (s Store) legacyIdentityPath() string {
	return filepath.Join(s.directory(), "identity.txt")
}
func (s Store) pendingIdentityPath() string {
	return filepath.Join(s.directory(), ".identity-pq-pending")
}

func (s Store) RecoveryDirectory() string { return filepath.Join(s.directory(), "recovery") }

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrIdentityMissing
	}
	if err != nil {
		return err
	}
	if !platformfs.DirectoryPrivate(path, info) {
		return ErrInvalidIdentity
	}
	return nil
}

func (s Store) ExistingIdentityPath(format archivekey.Format) (string, error) {
	if !filepath.IsAbs(s.Data) {
		return "", ErrInvalidIdentity
	}
	if err := privateDirectory(s.Data); err != nil {
		return "", err
	}
	if err := privateDirectory(s.directory()); err != nil {
		return "", err
	}
	var path string
	var err error
	switch format {
	case archivekey.Hybrid:
		path = s.identityPath()
		_, err = readIdentity(path)
	case archivekey.X25519:
		path = s.legacyIdentityPath()
		var body []byte
		body, err = maintenance.Read(path, identityLimit)
		defer clear(body)
		if err == nil {
			_, err = age.ParseX25519Identity(strings.TrimSpace(string(body)))
		}
	default:
		return "", ErrInvalidIdentity
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrIdentityMissing
		}
		return "", ErrInvalidIdentity
	}
	return path, nil
}

func readIdentity(path string) (*age.HybridIdentity, error) {
	body, err := maintenance.Read(path, identityLimit)
	defer clear(body)
	if err != nil {
		return nil, err
	}
	identity, err := age.ParseHybridIdentity(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, ErrInvalidIdentity
	}
	return identity, nil
}

func (s Store) publishPendingIdentity() (Prepared, error) {
	pending := s.pendingIdentityPath()
	identity, err := readIdentity(pending)
	if errors.Is(err, os.ErrNotExist) {
		return Prepared{}, ErrIdentityMissing
	}
	if err != nil {
		return Prepared{}, ErrInvalidIdentity
	}
	if err := publishIdentity(pending, s.identityPath()); err != nil {
		return Prepared{}, err
	}
	return Prepared{Recipient: identity.Recipient().String(), Changed: true}, nil
}

func (s Store) Prepare(ctx context.Context) (Prepared, error) {
	if !filepath.IsAbs(s.Data) {
		return Prepared{}, errors.New("absolute data root required")
	}
	if err := ensurePrivateDirectory(s.Data); err != nil {
		return Prepared{}, err
	}
	release, err := maintenance.Acquire(ctx, s.Data)
	if err != nil {
		return Prepared{}, err
	}
	defer release()
	if err := ensurePrivateDirectory(s.directory()); err != nil {
		return Prepared{}, err
	}

	path := s.identityPath()
	pending := s.pendingIdentityPath()
	identity, err := readIdentity(path)
	if err == nil {
		if err := reconcilePendingIdentity(pending, path); err != nil {
			return Prepared{}, err
		}
		return Prepared{Recipient: identity.Recipient().String()}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Prepared{}, ErrInvalidIdentity
	}

	if prepared, pendingErr := s.publishPendingIdentity(); pendingErr == nil {
		return prepared, nil
	} else if !errors.Is(pendingErr, ErrIdentityMissing) {
		return Prepared{}, pendingErr
	}

	identity, err = age.GenerateHybridIdentity()
	if err != nil {
		return Prepared{}, err
	}
	body := []byte(identity.String() + "\n")
	defer clear(body)
	if err := stageIdentity(pending, body); err != nil {
		return Prepared{}, err
	}
	return s.publishPendingIdentity()
}
