package vault

import (
	"context"
	"crypto/hmac"
)

// Seal preserves public logical identities/versions and the storage generation.
// It changes protection state only; a following Set changes the actual value
// revision. The caller owns the launch mutation lease before publishing it.
func (p *Profile) Seal(vaultID string, master *Material) (*Material, error) {
	if err := p.Verify(nil); err != nil {
		return nil, err
	}
	if p.Sealed {
		return nil, ErrInvalid
	}
	q, err := p.clone()
	if err != nil {
		return nil, err
	}
	key, err := NewKey()
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			key.Destroy()
		}
	}()
	q.VaultID = vaultID
	q.Sealed = true
	q.KeyEpoch = 1
	q.WrappedKey, err = WrapKeyset(master, vaultID, q.ID, q.KeyEpoch, key)
	if err != nil {
		return nil, err
	}
	if err = q.Sign(key); err != nil {
		return nil, err
	}
	actual, err := UnwrapKeyset(master, vaultID, q.ID, q.KeyEpoch, q.WrappedKey)
	if err != nil {
		return nil, err
	}
	defer actual.Destroy()
	if !hmac.Equal(actual.data, key.data) {
		return nil, ErrIntegrity
	}
	if err = q.Verify(actual); err != nil {
		return nil, err
	}
	*p = *q
	keep = true
	return key, nil
}

func (p *Profile) Register(name string, kind ValueKind, key *Material) (bool, error) {
	return p.Batch(context.Background(), key, func(m *Mutation) error { _, err := m.Register(name, kind); return err })
}
func (p *Profile) RemoveEnv(env string, key *Material) (bool, error) {
	return p.Batch(context.Background(), key, func(m *Mutation) error { _, err := m.RemoveEnv(env); return err })
}
