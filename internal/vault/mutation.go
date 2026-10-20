package vault

import (
	"context"
	"time"
)

func (p *Profile) CreateEnv(env string, key *Material) (bool, error) {
	return p.Batch(context.Background(), key, func(m *Mutation) error { _, err := m.CreateEnv(env); return err })
}
func (p *Profile) Unset(name string, kind ValueKind, env string, key *Material) (bool, error) {
	return p.Batch(context.Background(), key, func(m *Mutation) error { _, err := m.Unset(name, kind, env); return err })
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
