package vault

import (
	"context"
	"crypto/rand"
	"io"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"golang.org/x/crypto/chacha20poly1305"
)

// rotateProfile changes encryption keys and key epoch while preserving logical
// IDs, value versions, storage generation, revision, TTL and bundle selection.
func (p *Profile) rotateProfile(master, oldKey, newKey *Material) (*Profile, error) {
	if !p.Sealed || p.KeyEpoch == ^uint64(0) {
		return nil, ErrInvalid
	}
	if err := p.Verify(oldKey); err != nil {
		return nil, err
	}
	q, err := p.clone()
	if err != nil {
		return nil, err
	}
	q.KeyEpoch++
	q.WrappedKey, err = WrapKeyset(master, q.VaultID, q.ID, q.KeyEpoch, newKey)
	if err != nil {
		return nil, err
	}
	encryption, err := derive(newKey, q.VaultID, q.ID, "encryption")
	if err != nil {
		return nil, err
	}
	defer clear(encryption)
	cipher, err := chacha20poly1305.NewX(encryption)
	if err != nil {
		return nil, ErrInvalid
	}
	for i := range p.Values {
		if p.Values[i].Kind != Secret {
			continue
		}
		for j, oldSlot := range p.Values[i].Slots {
			plain, err := p.open(p.Values[i], oldSlot, oldKey)
			if err != nil {
				return nil, err
			}
			slot := &q.Values[i].Slots[j]
			slot.Nonce = make([]byte, chacha20poly1305.NonceSizeX)
			if _, err = io.ReadFull(rand.Reader, slot.Nonce); err != nil {
				clear(plain)
				return nil, err
			}
			aad, err := q.aad(q.Values[i], *slot)
			if err != nil {
				clear(plain)
				return nil, err
			}
			slot.Ciphertext = cipher.Seal(nil, slot.Nonce, plain, aad)
			clear(plain)
		}
	}
	if err = q.Sign(newKey); err != nil {
		return nil, err
	}
	actual, err := UnwrapKeyset(master, q.VaultID, q.ID, q.KeyEpoch, q.WrappedKey)
	if err != nil {
		return nil, err
	}
	defer actual.Destroy()
	if err = q.Verify(actual); err != nil {
		return nil, err
	}
	// Authenticate every rewritten secret slot before committing any file.
	for _, value := range q.Values {
		if value.Kind != Secret {
			continue
		}
		for _, slot := range value.Slots {
			plain, err := q.open(value, slot, actual)
			clear(plain)
			if err != nil {
				return nil, err
			}
		}
	}
	return q, nil
}

// RotateProfile is local key rotation, not provider credential rotation. It
// drains actual launch outcomes before taking disk maintenance, then publishes
// the profile and its authenticated catalog together. Callers obtain root keys
// before entering this method; it performs no native keyring interaction.
func (s FileStore) RotateProfile(ctx context.Context, name string, master *Material) (*Profile, error) {
	if !namePattern.MatchString(name) {
		return nil, ErrInvalid
	}
	releaseLaunch, err := AcquireLaunchMutation(ctx, filepath.Join(s.Root, "vault"))
	if err != nil {
		return nil, err
	}
	defer releaseLaunch()
	release, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		return nil, err
	}
	defer release()
	catalog, err := s.readCatalog()
	if err != nil {
		return nil, err
	}
	p, index, err := s.readProfileEnvelope(catalog, name)
	if err != nil {
		return nil, err
	}
	if !p.Sealed {
		return nil, ErrInvalid
	}
	oldKey, err := UnwrapKeyset(master, p.VaultID, p.ID, p.KeyEpoch, p.WrappedKey)
	if err != nil {
		return nil, err
	}
	defer oldKey.Destroy()
	historyFiles, err := s.receiptHistoryFiles(ctx, p, oldKey, time.Now())
	if err != nil {
		return nil, err
	}
	newKey, err := NewKey()
	if err != nil {
		return nil, err
	}
	defer newKey.Destroy()
	q, err := p.rotateProfile(master, oldKey, newKey)
	if err != nil {
		return nil, err
	}
	catalog.Entries[index] = entryFor(q)
	if err = s.publishWithFiles(ctx, catalog, q, newKey, historyFiles); err != nil {
		return nil, err
	}
	return q, nil
}
