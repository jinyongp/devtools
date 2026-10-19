package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// These wire types are private to the explicit age backup boundary. They are
// never public metadata, disk recovery state, or an agent-facing JSON response.
type logicalProfile struct {
	Format  int            `json:"logical_values_format"`
	Profile string         `json:"profile"`
	Sealed  *bool          `json:"sealed"`
	Envs    []string       `json:"envs"`
	Values  []logicalValue `json:"values"`
	Bundles []Bundle       `json:"bundles"`
}
type logicalValue struct {
	ID    string        `json:"value_id"`
	Name  string        `json:"name"`
	Kind  ValueKind     `json:"kind"`
	Slots []logicalSlot `json:"slots"`
}
type logicalSlot struct {
	ID        string  `json:"slot_id"`
	Env       string  `json:"env"`
	Version   string  `json:"value_version"`
	ExpiresAt *int64  `json:"expires_at,omitempty"`
	Value     *string `json:"value"`
}

// ExportLogical verifies the complete manifest and every slot, including
// overridden and expired slots. The caller must encrypt this owned material
// directly with age and destroy it; it must never become a plaintext file.
func (p *Profile) ExportLogical(key *Material) (*Material, error) {
	if err := p.Verify(key); err != nil {
		return nil, err
	}
	sealed := p.Sealed
	wire := logicalProfile{Format: 1, Profile: p.Name, Sealed: &sealed, Envs: append([]string{}, p.Envs...), Values: []logicalValue{}, Bundles: append([]Bundle{}, p.Bundles...)}
	total := 0
	for _, value := range p.Values {
		v := logicalValue{ID: value.ID, Name: value.Name, Kind: value.Kind, Slots: []logicalSlot{}}
		for _, slot := range value.Slots {
			plain, err := p.open(value, slot, key)
			if err != nil {
				return nil, err
			}
			total += len(plain)
			if total > MaxProtectedPlainBytes {
				clear(plain)
				return nil, ErrTooLarge
			}
			text := string(plain)
			v.Slots = append(v.Slots, logicalSlot{ID: slot.ID, Env: slot.Env, Version: slot.Version, ExpiresAt: slot.ExpiresAt, Value: &text})
			clear(plain)
		}
		wire.Values = append(wire.Values, v)
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, ErrInvalid
	}
	defer clear(b)
	if len(b) > MaxProtectedPlainBytes {
		return nil, ErrTooLarge
	}
	return NewMaterial(b), nil
}

// ImportLogical allocates a fresh local profile identity/generation/keyset and
// preserves logical value/slot versions and bundles. It never imports an OS key,
// session, launch handle, process record, or source vault identity. It prepares
// an in-memory candidate; publication still needs the launch mutation barrier.
func ImportLogical(encoded *Material, target, vaultID string, master *Material) (*Profile, *Material, error) {
	decoded, err := decodeLogical(encoded)
	if err != nil {
		return nil, nil, err
	}
	wire := *decoded
	p, err := NewProfile(target)
	if err != nil {
		return nil, nil, err
	}
	var key *Material
	for _, value := range wire.Values {
		if value.Kind == Secret && !*wire.Sealed {
			return nil, nil, ErrInvalid
		}
	}
	if *wire.Sealed {
		key, err = NewKey()
		if err != nil {
			return nil, nil, err
		}
		p, err = NewSealedProfile(target, vaultID, master, key)
		if err != nil {
			key.Destroy()
			return nil, nil, err
		}
	}
	keep := false
	defer func() {
		if !keep {
			key.Destroy()
		}
	}()
	p.Envs = append([]string{}, wire.Envs...)
	p.Bundles = append([]Bundle{}, wire.Bundles...)
	for _, value := range wire.Values {
		v := Value{ID: value.ID, Name: value.Name, Kind: value.Kind, Slots: []Slot{}}
		for _, slot := range value.Slots {
			s := Slot{ID: slot.ID, Env: slot.Env, Version: slot.Version, ExpiresAt: slot.ExpiresAt}
			if slot.Value == nil || !validText(*slot.Value) {
				return nil, nil, ErrInvalid
			}
			switch value.Kind {
			case Public:
				text := *slot.Value
				s.PublicValue = &text
			case Secret:
				k, err := derive(key, p.VaultID, p.ID, "encryption")
				if err != nil {
					return nil, nil, err
				}
				cipher, err := chacha20poly1305.NewX(k)
				clear(k)
				if err != nil {
					return nil, nil, ErrInvalid
				}
				s.Nonce = make([]byte, chacha20poly1305.NonceSizeX)
				if _, err = io.ReadFull(rand.Reader, s.Nonce); err != nil {
					return nil, nil, err
				}
				aad, err := p.aad(v, s)
				if err != nil {
					return nil, nil, err
				}
				plain := []byte(*slot.Value)
				s.Ciphertext = cipher.Seal(nil, s.Nonce, plain, aad)
				clear(plain)
			default:
				return nil, nil, ErrInvalid
			}
			v.Slots = append(v.Slots, s)
		}
		p.Values = append(p.Values, v)
	}
	if err = p.Sign(key); err != nil {
		return nil, nil, err
	}
	if _, err = p.Encode(); err != nil {
		return nil, nil, err
	}
	// Independently reopen all layers and compare logical contents, rather than
	// treating successful serialization as successful restore.
	check, err := p.ExportLogical(key)
	if err != nil {
		return nil, nil, err
	}
	defer check.Destroy()
	var actual logicalProfile
	if StrictDecode(check.data, &actual) != nil {
		return nil, nil, ErrIntegrity
	}
	actual.Profile = wire.Profile
	a, err := json.Marshal(actual)
	if err != nil {
		return nil, nil, ErrInvalid
	}
	defer clear(a)
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, nil, ErrInvalid
	}
	defer clear(b)
	if !bytes.Equal(a, b) {
		return nil, nil, ErrIntegrity
	}
	// A fresh profile's effective values are allowed to remain expired. Restore
	// must retain expiry rather than silently extending provider credentials.
	keep = true
	return p, key, nil
}
