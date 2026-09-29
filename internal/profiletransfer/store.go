// Package profiletransfer manages the local private identity used for cross-device profile transfer.
package profiletransfer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"filippo.io/age"
	"github.com/jinyongp/devtools/internal/maintenance"
)

const (
	identityLimit           = 4096
	pendingIdentityFilename = ".identity-pending"
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
func (s Store) identityPath() string { return filepath.Join(s.directory(), "identity.txt") }
func (s Store) pendingIdentityPath() string {
	return filepath.Join(s.directory(), pendingIdentityFilename)
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
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrInvalidIdentity
	}
	return nil
}

func (s Store) ExistingIdentityPath() (string, error) {
	if !filepath.IsAbs(s.Data) {
		return "", ErrInvalidIdentity
	}
	if err := privateDirectory(s.Data); err != nil {
		return "", err
	}
	if err := privateDirectory(s.directory()); err != nil {
		return "", err
	}
	path := s.identityPath()
	if _, err := readIdentity(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrIdentityMissing
		}
		return "", ErrInvalidIdentity
	}
	return path, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if err := privateDirectory(path); err != nil {
		return err
	}
	// Persist the directory entry too: syncing identity.txt's containing
	// directory alone cannot make a newly created transfer directory durable.
	// Repeat this on reuse so a previous uncertain publication can finish.
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	return syncDirectory(path)
}

func readIdentity(path string) (*age.X25519Identity, error) {
	body, err := maintenance.Read(path, identityLimit)
	if err != nil {
		return nil, err
	}
	identity, err := age.ParseX25519Identity(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, ErrInvalidIdentity
	}
	return identity, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func stageIdentity(path string, identity *age.X25519Identity) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	body := []byte(identity.String() + "\n")
	_, writeErr := file.Write(body)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		_ = syncDirectory(filepath.Dir(path))
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		_ = syncDirectory(filepath.Dir(path))
		return closeErr
	}
	return syncDirectory(filepath.Dir(path))
}

func publishPendingIdentity(pending, target string) (*age.X25519Identity, error) {
	identity, err := readIdentity(pending)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrIdentityMissing
		}
		return nil, ErrInvalidIdentity
	}
	if err := os.Link(pending, target); err != nil {
		return nil, err
	}
	directory := filepath.Dir(target)
	if err := syncDirectory(directory); err != nil {
		return nil, err
	}
	if err := os.Remove(pending); err != nil {
		return nil, err
	}
	if err := syncDirectory(directory); err != nil {
		return nil, err
	}
	return identity, nil
}

func reconcilePendingIdentity(pending, target string) error {
	pendingIdentity, err := readIdentity(pending)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || pendingIdentity == nil {
		return ErrInvalidIdentity
	}
	targetInfo, err := os.Lstat(target)
	if err != nil {
		return ErrInvalidIdentity
	}
	pendingInfo, err := os.Lstat(pending)
	if err != nil || !os.SameFile(targetInfo, pendingInfo) {
		return ErrInvalidIdentity
	}
	if err := os.Remove(pending); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
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

	if published, pendingErr := publishPendingIdentity(pending, path); pendingErr == nil {
		return Prepared{Recipient: published.Recipient().String(), Changed: true}, nil
	} else if !errors.Is(pendingErr, ErrIdentityMissing) {
		return Prepared{}, pendingErr
	}

	identity, err = age.GenerateX25519Identity()
	if err != nil {
		return Prepared{}, err
	}
	if err := stageIdentity(pending, identity); err != nil {
		return Prepared{}, err
	}
	published, err := publishPendingIdentity(pending, path)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Recipient: published.Recipient().String(), Changed: true}, nil
}
