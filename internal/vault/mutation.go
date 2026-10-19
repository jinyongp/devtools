package vault

import "time"

func (p *Profile) mutate(key *Material, change func(*Profile) (bool, error)) (bool, error) {
	if err := p.Verify(key); err != nil {
		return false, err
	}
	q, err := p.clone()
	if err != nil {
		return false, err
	}
	changed, err := change(q)
	if err != nil || !changed {
		return false, err
	}
	if q.Revision == ^uint64(0) {
		return false, ErrInvalid
	}
	q.Revision++
	if err = q.Sign(key); err != nil {
		return false, err
	}
	if _, err = q.Encode(); err != nil {
		return false, err
	}
	*p = *q
	return true, nil
}
func (p *Profile) CreateEnv(env string, key *Material) (bool, error) {
	return p.mutate(key, func(q *Profile) (bool, error) {
		if !namePattern.MatchString(env) {
			return false, ErrInvalid
		}
		for _, e := range q.Envs {
			if e == env {
				return false, nil
			}
		}
		q.Envs = append(q.Envs, env)
		return true, nil
	})
}
func (p *Profile) Unset(name string, kind ValueKind, env string, key *Material) (bool, error) {
	return p.mutate(key, func(q *Profile) (bool, error) {
		if err := q.checkEnv(env); err != nil {
			return false, err
		}
		for i := range q.Values {
			v := &q.Values[i]
			if v.Name != name {
				continue
			}
			if v.Kind != kind {
				return false, errorsNamed("kind_conflict")
			}
			for j, s := range v.Slots {
				if s.Env == env {
					v.Slots = append(v.Slots[:j], v.Slots[j+1:]...)
					return true, nil
				}
			}
			return false, nil
		}
		return false, nil
	})
}

// PublicValue permits explicit unverified reads while a sealed profile is locked.
// Its verified flag must be preserved in public responses; it is not an execution
// snapshot and cannot authorize a mutation or restart.
func (p *Profile) PublicValue(name, env string, key *Material, now time.Time) (string, bool, error) {
	if err := p.validate(); err != nil {
		return "", false, err
	}
	verified := false
	if p.Sealed && key != nil {
		if err := p.Verify(key); err != nil {
			return "", false, err
		}
		verified = true
	}
	if err := p.checkEnv(env); err != nil {
		return "", false, err
	}
	for _, v := range p.Values {
		if v.Name != name {
			continue
		}
		if v.Kind != Public {
			return "", false, errorsNamed("kind_conflict")
		}
		var chosen *Slot
		for i := range v.Slots {
			s := &v.Slots[i]
			if s.Env == "" {
				chosen = s
			}
		}
		if env != "" {
			for i := range v.Slots {
				s := &v.Slots[i]
				if s.Env == env {
					chosen = s
					break
				}
			}
		}
		if chosen == nil {
			break
		}
		if chosen.ExpiresAt != nil && now.Unix() >= *chosen.ExpiresAt {
			return "", verified, errorsNamed("value_expired")
		}
		return *chosen.PublicValue, verified, nil
	}
	return "", verified, errorsNamed("key_not_found")
}
