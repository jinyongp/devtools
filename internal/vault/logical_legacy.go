package vault

import "encoding/json"

// decodeLogical validates the portable structure before allocating encryption
// keys or treating absent fields as empty values. age authenticity is checked
// by the enclosing backup reader, not by this in-memory structural validator.
func decodeLogical(encoded *Material) (*logicalProfile, error) {
	if encoded == nil {
		return nil, ErrInvalid
	}
	if len(encoded.data) > MaxProtectedPlainBytes {
		return nil, ErrTooLarge
	}
	var w logicalProfile
	if StrictDecode(encoded.data, &w) != nil || w.Sealed == nil || w.Format != 1 || !namePattern.MatchString(w.Profile) || w.Envs == nil || w.Values == nil || w.Bundles == nil {
		return nil, ErrInvalid
	}
	dummy := "00000000000000000000000000000000"
	p := &Profile{Format: StorageVersion, Name: w.Profile, ID: dummy, Generation: dummy, Revision: 1, Envs: w.Envs, Values: []Value{}, Bundles: w.Bundles, Sealed: *w.Sealed}
	if p.Sealed {
		p.VaultID = dummy
		p.KeyEpoch = 1
		p.WrappedKey = make([]byte, wrappedKeySize)
	}
	for _, value := range w.Values {
		if value.Slots == nil {
			return nil, ErrInvalid
		}
		v := Value{ID: value.ID, Name: value.Name, Kind: value.Kind, Slots: []Slot{}}
		for _, slot := range value.Slots {
			if slot.Value == nil || !validText(*slot.Value) {
				return nil, ErrInvalid
			}
			s := Slot{ID: slot.ID, Env: slot.Env, Version: slot.Version, ExpiresAt: slot.ExpiresAt}
			if value.Kind == Public {
				s.PublicValue = slot.Value
			} else {
				s.Nonce = make([]byte, 24)
				s.Ciphertext = make([]byte, 16)
			}
			v.Slots = append(v.Slots, s)
		}
		p.Values = append(p.Values, v)
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return &w, nil
}

// LogicalLegacyView is a deliberate private compatibility boundary for existing
// metadata/diff code. It does not authorize storing legacy plaintext or discard
// TTL/bundles from the actual archive: callers retain the original logical body.
func LogicalLegacyView(encoded *Material, source string) (*Material, error) {
	w, err := decodeLogical(encoded)
	if err != nil {
		return nil, err
	}
	if w.Profile != source {
		return nil, ErrInvalid
	}
	type legacyEntry struct {
		Kind   ValueKind         `json:"kind"`
		Common *string           `json:"common,omitempty"`
		Envs   map[string]string `json:"envs"`
	}
	state := struct {
		Version int                    `json:"version"`
		Profile string                 `json:"profile"`
		Envs    map[string]bool        `json:"envs"`
		Keys    map[string]legacyEntry `json:"keys"`
	}{Version: 1, Profile: source, Envs: map[string]bool{}, Keys: map[string]legacyEntry{}}
	for _, env := range w.Envs {
		state.Envs[env] = true
	}
	for _, value := range w.Values {
		entry := legacyEntry{Kind: value.Kind, Envs: map[string]string{}}
		for _, slot := range value.Slots {
			if slot.Env == "" {
				entry.Common = slot.Value
			} else {
				entry.Envs[slot.Env] = *slot.Value
			}
		}
		state.Keys[value.Name] = entry
	}
	b, err := json.Marshal(state)
	if err != nil {
		return nil, ErrInvalid
	}
	defer clear(b)
	if len(b) > MaxProtectedPlainBytes {
		return nil, ErrTooLarge
	}
	return NewMaterial(b), nil
}

func RenameLogical(encoded *Material, source, target string) (*Material, error) {
	w, err := decodeLogical(encoded)
	if err != nil {
		return nil, err
	}
	if w.Profile != source || !namePattern.MatchString(target) {
		return nil, ErrInvalid
	}
	w.Profile = target
	b, err := json.Marshal(w)
	if err != nil {
		return nil, ErrInvalid
	}
	defer clear(b)
	if len(b) > MaxProtectedPlainBytes {
		return nil, ErrTooLarge
	}
	return NewMaterial(b), nil
}
