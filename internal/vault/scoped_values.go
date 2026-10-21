package vault

import "time"

// ResolveScopedValues checks the complete selection ceiling before opening any
// value. Missing effective slots are errors, including for bundle members.
func (p *Profile) ResolveScopedValues(env string, key *Material, now time.Time, keys []string, bundle string) ([]EffectiveValue, string, error) {
	if keys == nil || len(keys) > 5000 || bundle != "" && !ValidSelection(bundle) {
		return nil, "", ErrInvalid
	}
	allowed := map[string]bool{}
	for _, name := range keys {
		if !valueNamePattern.MatchString(name) || allowed[name] {
			return nil, "", ErrInvalid
		}
		allowed[name] = true
	}
	if err := p.Verify(key); err != nil {
		return nil, "", err
	}
	if err := p.checkEnv(env); err != nil {
		return nil, "", err
	}
	var selected map[string]bool
	var bundleID string
	if bundle != "" {
		var err error
		selected, err = p.selectionIDs(bundle)
		if err != nil {
			return nil, "", err
		}
		for _, b := range p.Bundles {
			if b.Name == bundle {
				bundleID = b.ID
			}
		}
	}
	names := map[string]bool{}
	for _, v := range p.Values {
		include := allowed[v.Name]
		if bundle != "" {
			include = selected == nil || selected[v.ID]
		}
		if !include {
			continue
		}
		if !allowed[v.Name] {
			return nil, "", errorsNamed("selection_outside_scope")
		}
		names[v.Name] = true
	}
	if bundle == "" && len(names) != len(allowed) {
		return nil, "", errorsNamed("key_not_found")
	}
	// Resolve metadata first so absent/expired selected slots fail before any
	// secret is decrypted. Never intersect a bundle silently with the ceiling.
	metadata, err := p.ResolveSelectedMetadata(env, key, "all")
	if err != nil {
		return nil, "", err
	}
	byName := map[string]EffectiveValue{}
	for _, v := range metadata {
		byName[v.Reference.Name] = v
	}
	for name := range names {
		v, ok := byName[name]
		if !ok {
			return nil, "", errorsNamed("key_not_found")
		}
		if v.ExpiresAt != nil && now.Unix() >= *v.ExpiresAt {
			return nil, "", errorsNamed("value_expired")
		}
	}
	out := []EffectiveValue{}
	valuesByID := make(map[string]Value, len(p.Values))
	for _, v := range p.Values {
		valuesByID[v.ID] = v
	}
	for _, meta := range metadata {
		if !names[meta.Reference.Name] {
			continue
		}
		v := valuesByID[meta.Reference.ValueID]
		for _, slot := range v.Slots {
			if slot.ID != meta.Reference.SlotID {
				continue
			}
			plain, err := p.open(v, slot, key)
			if err != nil {
				return nil, "", err
			}
			meta.Value = string(plain)
			clear(plain)
		}
		out = append(out, meta)
	}
	return out, bundleID, nil
}
