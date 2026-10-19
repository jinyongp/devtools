package vault

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var valueNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type ValueKind string

const (
	Public ValueKind = "variable"
	Secret ValueKind = "secret"
)

// Profile is the disk representation. Secret plaintext is never a field here.
// Call Verify before using any sealed profile fields for execution or mutation.
type Profile struct {
	Format     int      `json:"vault_format"`
	VaultID    string   `json:"vault_id"`
	ID         string   `json:"profile_id"`
	Name       string   `json:"profile"`
	Generation string   `json:"storage_generation"`
	Revision   uint64   `json:"revision"`
	KeyEpoch   uint64   `json:"key_epoch"`
	Sealed     bool     `json:"sealed"`
	WrappedKey []byte   `json:"wrapped_key,omitempty"`
	Envs       []string `json:"envs"`
	Values     []Value  `json:"values"`
	Bundles    []Bundle `json:"bundles"`
	Tag        []byte   `json:"manifest_tag,omitempty"`
}
type Value struct {
	ID    string    `json:"value_id"`
	Name  string    `json:"name"`
	Kind  ValueKind `json:"kind"`
	Slots []Slot    `json:"slots"`
}
type Slot struct {
	ID          string  `json:"slot_id"`
	Env         string  `json:"env"`
	Version     string  `json:"value_version"`
	ExpiresAt   *int64  `json:"expires_at,omitempty"`
	PublicValue *string `json:"public_value,omitempty"`
	Nonce       []byte  `json:"nonce,omitempty"`
	Ciphertext  []byte  `json:"ciphertext,omitempty"`
}
type Bundle struct {
	ID       string   `json:"bundle_id"`
	Name     string   `json:"name"`
	ValueIDs []string `json:"value_ids"`
}
type SlotReference struct {
	Name    string `json:"name"`
	ValueID string `json:"value_id"`
	SlotID  string `json:"slot_id"`
	Version string `json:"value_version"`
}

func NewID() (string, error) { return randomID(rand.Reader) }
func randomID(r io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	b, err := hex.DecodeString(id)
	return err == nil && hex.EncodeToString(b) == id
}
func validText(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

func NewProfile(name string) (*Profile, error) {
	if !namePattern.MatchString(name) {
		return nil, ErrInvalid
	}
	id, err := NewID()
	if err != nil {
		return nil, err
	}
	gen, err := NewID()
	if err != nil {
		return nil, err
	}
	return &Profile{Format: StorageVersion, Name: name, ID: id, Generation: gen, Revision: 1, Envs: []string{}, Values: []Value{}, Bundles: []Bundle{}}, nil
}

// NewSealedProfile expects an already protected profile keyset. The wrapper is
// authenticated with the profile, while key unwrapping belongs to the protector.
func NewSealedProfile(name, vaultID string, wrapped []byte, key *Material) (*Profile, error) {
	p, err := NewProfile(name)
	if err != nil {
		return nil, err
	}
	p.VaultID = vaultID
	p.Sealed = true
	p.KeyEpoch = 1
	p.WrappedKey = append([]byte(nil), wrapped...)
	if err = p.Sign(key); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Profile) validate() error {
	if p == nil || p.Format != StorageVersion || !namePattern.MatchString(p.Name) || !validID(p.ID) || !validID(p.Generation) || p.Revision == 0 {
		return ErrInvalid
	}
	if p.Sealed {
		if !validID(p.VaultID) || p.KeyEpoch == 0 || len(p.WrappedKey) == 0 {
			return ErrInvalid
		}
	} else if p.VaultID != "" || p.KeyEpoch != 0 || len(p.WrappedKey) != 0 || len(p.Tag) != 0 {
		return ErrInvalid
	}
	envs := map[string]bool{"": true}
	for _, env := range p.Envs {
		if env == "" || !namePattern.MatchString(env) || envs[env] {
			return ErrInvalid
		}
		envs[env] = true
	}
	ids, names, slots := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range p.Values {
		if !validID(v.ID) || ids[v.ID] || !valueNamePattern.MatchString(v.Name) || names[v.Name] || (v.Kind != Public && v.Kind != Secret) || (!p.Sealed && v.Kind == Secret) {
			return ErrInvalid
		}
		ids[v.ID] = true
		names[v.Name] = true
		scopes := map[string]bool{}
		for _, s := range v.Slots {
			if !validID(s.ID) || slots[s.ID] || !validID(s.Version) || !envs[s.Env] || scopes[s.Env] || (s.ExpiresAt != nil && *s.ExpiresAt < 0) {
				return ErrInvalid
			}
			slots[s.ID] = true
			scopes[s.Env] = true
			if v.Kind == Public {
				if s.PublicValue == nil || !validText(*s.PublicValue) || len(s.Nonce) != 0 || len(s.Ciphertext) != 0 {
					return ErrInvalid
				}
			} else if s.PublicValue != nil || len(s.Nonce) != chacha20poly1305.NonceSizeX || len(s.Ciphertext) < chacha20poly1305.Overhead {
				return ErrInvalid
			}
		}
	}
	bundleIDs, bundleNames := map[string]bool{}, map[string]bool{}
	for _, b := range p.Bundles {
		if !validID(b.ID) || bundleIDs[b.ID] || !namePattern.MatchString(b.Name) || b.Name == "all" || b.Name == "none" || bundleNames[b.Name] {
			return ErrInvalid
		}
		bundleIDs[b.ID] = true
		bundleNames[b.Name] = true
		members := map[string]bool{}
		for _, id := range b.ValueIDs {
			if !ids[id] || members[id] {
				return ErrInvalid
			}
			members[id] = true
		}
	}
	return nil
}

func derive(key *Material, vaultID, profileID, purpose string) ([]byte, error) {
	b := key.CopyBytes()
	defer clear(b)
	if len(b) != 32 {
		return nil, ErrLocked
	}
	e := NewEncoder("key-info")
	e.Uint(SessionProtocol)
	e.String(vaultID)
	e.String(profileID)
	e.String(purpose)
	info, err := e.Finish()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 32)
	_, err = io.ReadFull(hkdf.New(sha256.New, b, nil, info), out)
	if err != nil {
		clear(out)
		return nil, ErrIntegrity
	}
	return out, nil
}

func (p *Profile) manifest() ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	e := NewEncoder("profile-manifest")
	e.Uint(uint64(p.Format))
	e.String(p.VaultID)
	e.String(p.ID)
	e.String(p.Name)
	e.String(p.Generation)
	e.Uint(p.Revision)
	e.Uint(p.KeyEpoch)
	e.Bool(p.Sealed)
	e.Blob(p.WrappedKey)
	envs := append([]string{}, p.Envs...)
	sort.Strings(envs)
	e.Uint(uint64(len(envs)))
	for _, env := range envs {
		e.String(env)
	}
	values := append([]Value{}, p.Values...)
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	e.Uint(uint64(len(values)))
	for _, v := range values {
		e.String(v.ID)
		e.String(v.Name)
		e.String(string(v.Kind))
		slots := append([]Slot{}, v.Slots...)
		sort.Slice(slots, func(i, j int) bool { return slots[i].ID < slots[j].ID })
		e.Uint(uint64(len(slots)))
		for _, s := range slots {
			e.Bool(true)
			e.String(s.ID)
			e.String(s.Env)
			e.String(s.Version)
			e.Bool(s.ExpiresAt != nil)
			if s.ExpiresAt != nil {
				e.Uint(uint64(*s.ExpiresAt))
			}
			e.Bool(s.PublicValue != nil)
			if s.PublicValue != nil {
				e.String(*s.PublicValue)
			}
			e.Blob(s.Nonce)
			e.Blob(s.Ciphertext)
		}
	}
	bundles := append([]Bundle{}, p.Bundles...)
	sort.Slice(bundles, func(i, j int) bool { return bundles[i].ID < bundles[j].ID })
	e.Uint(uint64(len(bundles)))
	for _, b := range bundles {
		e.String(b.ID)
		e.String(b.Name)
		ids := append([]string{}, b.ValueIDs...)
		sort.Strings(ids)
		e.Uint(uint64(len(ids)))
		for _, id := range ids {
			e.String(id)
		}
	}
	return e.Finish()
}
func (p *Profile) Sign(key *Material) error {
	data, err := p.manifest()
	if err != nil {
		return err
	}
	if !p.Sealed {
		return nil
	}
	k, err := derive(key, p.VaultID, p.ID, "manifest")
	if err != nil {
		return err
	}
	defer clear(k)
	h := hmac.New(sha256.New, k)
	h.Write(data)
	p.Tag = h.Sum(nil)
	return nil
}
func (p *Profile) Verify(key *Material) error {
	data, err := p.manifest()
	if err != nil {
		return err
	}
	if !p.Sealed {
		return nil
	}
	k, err := derive(key, p.VaultID, p.ID, "manifest")
	if err != nil {
		return err
	}
	defer clear(k)
	h := hmac.New(sha256.New, k)
	h.Write(data)
	if !hmac.Equal(p.Tag, h.Sum(nil)) {
		return ErrIntegrity
	}
	return nil
}
func (p *Profile) Encode() ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	if p.Sealed && len(p.Tag) != sha256.Size {
		return nil, ErrIntegrity
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, ErrInvalid
	}
	if len(b) > MaxDocumentBytes {
		return nil, ErrTooLarge
	}
	return b, nil
}
func DecodeProfile(data []byte) (*Profile, error) {
	var p Profile
	if err := StrictDecode(data, &p); err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return &p, nil
}
func (p *Profile) clone() (*Profile, error) {
	b, err := p.Encode()
	if err != nil {
		return nil, err
	}
	return DecodeProfile(b)
}
func (p *Profile) checkEnv(env string) error {
	if env == "" {
		return nil
	}
	for _, e := range p.Envs {
		if e == env {
			return nil
		}
	}
	return errorsNamed("env_not_found")
}

// A bounded fixed error never includes a caller-provided value.
func errorsNamed(code string) error { return &valueError{code} }

type valueError struct{ code string }

func (e *valueError) Error() string { return e.code }

func (p *Profile) aad(v Value, s Slot) ([]byte, error) {
	e := NewEncoder("slot-aad")
	e.Uint(uint64(p.Format))
	e.String(p.VaultID)
	e.String(p.ID)
	e.String(v.ID)
	e.String(s.ID)
	e.String(s.Version)
	e.Uint(p.KeyEpoch)
	return e.Finish()
}
func (p *Profile) open(v Value, s Slot, key *Material) ([]byte, error) {
	if v.Kind == Public {
		return []byte(*s.PublicValue), nil
	}
	k, err := derive(key, p.VaultID, p.ID, "encryption")
	if err != nil {
		return nil, err
	}
	defer clear(k)
	a, err := chacha20poly1305.NewX(k)
	if err != nil {
		return nil, ErrIntegrity
	}
	aad, err := p.aad(v, s)
	if err != nil {
		return nil, err
	}
	b, err := a.Open(nil, s.Nonce, s.Ciphertext, aad)
	if err != nil {
		return nil, ErrIntegrity
	}
	return b, nil
}

func (p *Profile) Set(name string, kind ValueKind, env, value string, key *Material) (bool, error) {
	return p.set(name, kind, env, value, key, rand.Reader)
}
func (p *Profile) set(name string, kind ValueKind, env, value string, key *Material, r io.Reader) (bool, error) {
	if err := p.Verify(key); err != nil {
		return false, err
	}
	if !valueNamePattern.MatchString(name) || !validText(value) || (kind != Public && kind != Secret) {
		return false, ErrInvalid
	}
	if len(value) > MaxDocumentBytes {
		return false, ErrTooLarge
	}
	if err := p.checkEnv(env); err != nil {
		return false, err
	}
	if kind == Secret && !p.Sealed {
		return false, ErrLocked
	}
	q, err := p.clone()
	if err != nil {
		return false, err
	}
	vi := -1
	for i, v := range q.Values {
		if v.Name == name {
			if v.Kind != kind {
				return false, errorsNamed("kind_conflict")
			}
			vi = i
			break
		}
	}
	if vi < 0 {
		id, err := randomID(r)
		if err != nil {
			return false, err
		}
		q.Values = append(q.Values, Value{ID: id, Name: name, Kind: kind, Slots: []Slot{}})
		vi = len(q.Values) - 1
	}
	v := &q.Values[vi]
	si := -1
	var slot Slot
	for i, s := range v.Slots {
		if s.Env == env {
			si = i
			slot = s
			break
		}
	}
	if si >= 0 {
		old, err := q.open(*v, slot, key)
		if err != nil {
			return false, err
		}
		same := bytes.Equal(old, []byte(value))
		clear(old)
		if same {
			return false, nil
		}
	} else {
		slot.ID, err = randomID(r)
		if err != nil {
			return false, err
		}
		slot.Env = env
	}
	slot.Version, err = randomID(r)
	if err != nil {
		return false, err
	}
	if kind == Public {
		s := value
		slot.PublicValue = &s
	} else {
		k, err := derive(key, q.VaultID, q.ID, "encryption")
		if err != nil {
			return false, err
		}
		defer clear(k)
		a, err := chacha20poly1305.NewX(k)
		if err != nil {
			return false, ErrIntegrity
		}
		nonce := make([]byte, a.NonceSize())
		if _, err = io.ReadFull(r, nonce); err != nil {
			return false, err
		}
		aad, err := q.aad(*v, slot)
		if err != nil {
			return false, err
		}
		plain := []byte(value)
		defer clear(plain)
		slot.Nonce = nonce
		slot.Ciphertext = a.Seal(nil, nonce, plain, aad)
	}
	if si >= 0 {
		v.Slots[si] = slot
	} else {
		v.Slots = append(v.Slots, slot)
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

// Environment verifies the whole snapshot before resolving a common/env layer.
// A present expired or damaged override is never replaced by common.
func (p *Profile) Environment(env string, key *Material, now time.Time) (map[string]string, []SlotReference, error) {
	if err := p.Verify(key); err != nil {
		return nil, nil, err
	}
	if err := p.checkEnv(env); err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	refs := []SlotReference{}
	for _, v := range p.Values {
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
			continue
		}
		if chosen.ExpiresAt != nil && now.Unix() >= *chosen.ExpiresAt {
			return nil, nil, errorsNamed("value_expired")
		}
		b, err := p.open(v, *chosen, key)
		if err != nil {
			return nil, nil, err
		}
		out[v.Name] = string(b)
		clear(b)
		refs = append(refs, SlotReference{Name: v.Name, ValueID: v.ID, SlotID: chosen.ID, Version: chosen.Version})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return out, refs, nil
}
