package vault

import "crypto/hmac"

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
	return p.mutate(key, func(q *Profile) (bool, error) {
		if !valueNamePattern.MatchString(name) || (kind != Public && kind != Secret) {
			return false, ErrInvalid
		}
		if kind == Secret && !q.Sealed {
			return false, ErrLocked
		}
		for _, value := range q.Values {
			if value.Name == name {
				if value.Kind != kind {
					return false, errorsNamed("kind_conflict")
				}
				return false, nil
			}
		}
		id, err := NewID()
		if err != nil {
			return false, err
		}
		q.Values = append(q.Values, Value{ID: id, Name: name, Kind: kind, Slots: []Slot{}})
		return true, nil
	})
}
func (p *Profile) RemoveEnv(env string, key *Material) (bool, error) {
	return p.mutate(key, func(q *Profile) (bool, error) {
		if !namePattern.MatchString(env) {
			return false, ErrInvalid
		}
		if err := q.checkEnv(env); err != nil {
			return false, err
		}
		for _, value := range q.Values {
			for _, slot := range value.Slots {
				if slot.Env == env {
					return false, errorsNamed("env_not_empty")
				}
			}
		}
		for i, name := range q.Envs {
			if name == env {
				q.Envs = append(q.Envs[:i], q.Envs[i+1:]...)
				return true, nil
			}
		}
		return false, nil
	})
}
