package vault

import (
	"context"
	"time"
)

// Empty selection preserves the profile-wide default. Names do not encode an
// env: bundles select ValueIDs, then the selected env resolves their slots.
func ValidSelection(selection string) bool {
	return selection == "" || namePattern.MatchString(selection)
}
func ValidBundleName(name string) bool {
	return namePattern.MatchString(name) && name != "all" && name != "none"
}

// SetBundle atomically replaces membership by shared ValueID.
func (p *Profile) SetBundle(name string, names []string, key *Material) (bool, error) {
	return p.Batch(context.Background(), key, func(m *Mutation) error { _, err := m.SetBundle(name, names); return err })
}
func (p *Profile) RenameBundle(name, next string, key *Material) (bool, error) {
	return p.Batch(context.Background(), key, func(m *Mutation) error { _, err := m.RenameBundle(name, next); return err })
}
func (p *Profile) RemoveBundle(name string, key *Material) (bool, error) {
	return p.Batch(context.Background(), key, func(m *Mutation) error { _, err := m.RemoveBundle(name); return err })
}
func (p *Profile) selectionIDs(selection string) (map[string]bool, error) {
	if selection == "" || selection == "all" {
		return nil, nil
	}
	if !ValidSelection(selection) {
		return nil, ErrInvalid
	}
	ids := map[string]bool{}
	if selection == "none" {
		return ids, nil
	}
	for _, bundle := range p.Bundles {
		if bundle.Name == selection {
			for _, id := range bundle.ValueIDs {
				ids[id] = true
			}
			return ids, nil
		}
	}
	return nil, errorsNamed("bundle_not_found")
}

// ResolveSelectedValues authenticates the whole manifest before selecting any
// values. Unselected secrets are not decrypted and their expiry does not block
// this execution; selected expired overrides still fail without common fallback.
func (p *Profile) ResolveSelectedValues(env string, key *Material, now time.Time, includeSecrets bool, selection string) ([]EffectiveValue, error) {
	return p.resolveValues(env, key, now, includeSecrets, selection, false)
}

// ResolveSelectedMetadata authenticates the complete manifest and resolves the
// effective slots without opening any value, including public variables. Expiry
// is returned so callers can inspect only the actual delivered dependencies.
func (p *Profile) ResolveSelectedMetadata(env string, key *Material, selection string) ([]EffectiveValue, error) {
	return p.resolveValues(env, key, time.Time{}, false, selection, true)
}
