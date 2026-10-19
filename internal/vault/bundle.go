package vault

import (
	"reflect"
	"sort"
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

// SetBundle atomically replaces membership by shared ValueID. It never copies
// values, changes their slot versions, or permits nested bundles.
func (p *Profile) SetBundle(name string, names []string, key *Material) (bool, error) {
	if err := p.Verify(key); err != nil {
		return false, err
	}
	if !ValidBundleName(name) || len(names) > len(p.Values) {
		return false, ErrInvalid
	}
	members := map[string]bool{}
	byName := make(map[string]string, len(p.Values))
	for _, value := range p.Values {
		byName[value.Name] = value.ID
	}
	ids := []string{}
	for _, name := range names {
		if members[name] {
			return false, ErrInvalid
		}
		members[name] = true
		id, found := byName[name]
		if !found {
			return false, errorsNamed("key_not_found")
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	q, err := p.clone()
	if err != nil {
		return false, err
	}
	index := -1
	for i, bundle := range q.Bundles {
		if bundle.Name == name {
			index = i
			break
		}
	}
	if index >= 0 {
		old := append([]string{}, q.Bundles[index].ValueIDs...)
		sort.Strings(old)
		if reflect.DeepEqual(old, ids) {
			return false, nil
		}
		q.Bundles[index].ValueIDs = ids
	} else {
		id, err := NewID()
		if err != nil {
			return false, err
		}
		q.Bundles = append(q.Bundles, Bundle{ID: id, Name: name, ValueIDs: ids})
	}
	return p.publishBundleChange(q, key)
}
func (p *Profile) RenameBundle(name, next string, key *Material) (bool, error) {
	if err := p.Verify(key); err != nil {
		return false, err
	}
	if !ValidBundleName(next) {
		return false, ErrInvalid
	}
	index := -1
	for i, bundle := range p.Bundles {
		if bundle.Name == name {
			index = i
		}
		if bundle.Name == next && name != next {
			return false, errorsNamed("bundle_exists")
		}
	}
	if index < 0 {
		return false, errorsNamed("bundle_not_found")
	}
	if name == next {
		return false, nil
	}
	q, err := p.clone()
	if err != nil {
		return false, err
	}
	q.Bundles[index].Name = next
	return p.publishBundleChange(q, key)
}
func (p *Profile) RemoveBundle(name string, key *Material) (bool, error) {
	if err := p.Verify(key); err != nil {
		return false, err
	}
	index := -1
	for i, bundle := range p.Bundles {
		if bundle.Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return false, errorsNamed("bundle_not_found")
	}
	q, err := p.clone()
	if err != nil {
		return false, err
	}
	q.Bundles = append(q.Bundles[:index], q.Bundles[index+1:]...)
	return p.publishBundleChange(q, key)
}
func (p *Profile) publishBundleChange(q *Profile, key *Material) (bool, error) {
	if q.Revision == ^uint64(0) {
		return false, ErrInvalid
	}
	q.Revision++
	if err := q.Sign(key); err != nil {
		return false, err
	}
	if _, err := q.Encode(); err != nil {
		return false, err
	}
	*p = *q
	return true, nil
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
