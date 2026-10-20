package vault

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"reflect"
	"sort"

	"golang.org/x/crypto/chacha20poly1305"
)

// Mutation owns one private candidate. Only requested slots are opened; the
// complete manifest is authenticated before the candidate is constructed.
// A Mutation is valid only during its Batch callback and is not concurrent-safe.
type Mutation struct {
	p       *Profile
	key     *Material
	ctx     context.Context
	reader  io.Reader
	values  map[string]int
	bundles map[string]int
	changed bool
	observe func(string)
}

// Batch commits an in-memory candidate only after all operations, final
// validation and signing succeed. Disk publication belongs to FileStore.
func (p *Profile) Batch(ctx context.Context, key *Material, change func(*Mutation) error) (bool, error) {
	return p.batch(ctx, key, rand.Reader, change, nil)
}
func (p *Profile) batch(ctx context.Context, key *Material, r io.Reader, change func(*Mutation) error, observe func(string)) (bool, error) {
	step := func(name string) {
		if observe != nil {
			observe(name)
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	step("verify")
	if err := p.Verify(key); err != nil {
		return false, err
	}
	step("clone")
	q, err := p.clone()
	if err != nil {
		return false, err
	}
	m := &Mutation{p: q, key: key, ctx: ctx, reader: r, values: make(map[string]int, len(q.Values)), bundles: make(map[string]int, len(q.Bundles)), observe: observe}
	for i, v := range q.Values {
		m.values[v.Name] = i
	}
	for i, b := range q.Bundles {
		m.bundles[b.Name] = i
	}
	// Do not leave retained editors holding key/candidate references after return.
	defer func() { m.p = nil; m.key = nil }()
	if err = change(m); err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if !m.changed {
		return false, nil
	}
	if q.Revision == ^uint64(0) {
		return false, ErrInvalid
	}
	q.Revision++
	step("sign")
	if err = q.Sign(m.key); err != nil {
		return false, err
	}
	step("encode")
	if _, err = q.Encode(); err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	*p = *q
	return true, nil
}
func (m *Mutation) check() error {
	if m.p == nil {
		return ErrInvalid
	}
	return m.ctx.Err()
}

// Value reads one exact layer, including expired slots, for mutation comparison.
// Execution and metadata reads retain their separate effective/expiry contracts.
func (m *Mutation) Value(name, env string) (string, bool, error) {
	if err := m.check(); err != nil {
		return "", false, err
	}
	i, ok := m.values[name]
	if !ok {
		return "", false, nil
	}
	v := m.p.Values[i]
	for _, s := range v.Slots {
		if s.Env == env {
			if m.observe != nil && v.Kind == Secret {
				m.observe("open")
			}
			raw, err := m.p.open(v, s, m.key)
			if err != nil {
				return "", false, err
			}
			defer clear(raw)
			return string(raw), true, nil
		}
	}
	return "", false, nil
}
func (m *Mutation) Set(name string, kind ValueKind, env, value string) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	if !valueNamePattern.MatchString(name) || !validText(value) || (kind != Public && kind != Secret) {
		return false, ErrInvalid
	}
	if len(value) > MaxDocumentBytes {
		return false, ErrTooLarge
	}
	if err := m.p.checkEnv(env); err != nil {
		return false, err
	}
	if kind == Secret && !m.p.Sealed {
		return false, ErrLocked
	}
	vi, found := m.values[name]
	if found && m.p.Values[vi].Kind != kind {
		return false, errorsNamed("kind_conflict")
	}
	if !found {
		id, err := randomID(m.reader)
		if err != nil {
			return false, err
		}
		vi = len(m.p.Values)
		m.values[name] = vi
		m.p.Values = append(m.p.Values, Value{ID: id, Name: name, Kind: kind, Slots: []Slot{}})
	}
	v := &m.p.Values[vi]
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
		if m.observe != nil && kind == Secret {
			m.observe("open")
		}
		old, err := m.p.open(*v, slot, m.key)
		if err != nil {
			return false, err
		}
		same := bytes.Equal(old, []byte(value))
		clear(old)
		if same {
			return false, nil
		}
	} else {
		var err error
		slot.ID, err = randomID(m.reader)
		if err != nil {
			return false, err
		}
		slot.Env = env
	}
	var err error
	slot.Version, err = randomID(m.reader)
	if err != nil {
		return false, err
	}
	if kind == Public {
		text := value
		slot.PublicValue = &text
	} else {
		k, err := derive(m.key, m.p.VaultID, m.p.ID, "encryption")
		if err != nil {
			return false, err
		}
		defer clear(k)
		a, err := chacha20poly1305.NewX(k)
		if err != nil {
			return false, ErrIntegrity
		}
		slot.Nonce = make([]byte, a.NonceSize())
		if _, err = io.ReadFull(m.reader, slot.Nonce); err != nil {
			return false, err
		}
		aad, err := m.p.aad(*v, slot)
		if err != nil {
			return false, err
		}
		plain := []byte(value)
		defer clear(plain)
		slot.Ciphertext = a.Seal(nil, slot.Nonce, plain, aad)
	}
	if si >= 0 {
		v.Slots[si] = slot
	} else {
		v.Slots = append(v.Slots, slot)
	}
	m.changed = true
	return true, nil
}
func (m *Mutation) Register(name string, kind ValueKind) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	if !valueNamePattern.MatchString(name) || (kind != Public && kind != Secret) {
		return false, ErrInvalid
	}
	if kind == Secret && !m.p.Sealed {
		return false, ErrLocked
	}
	if i, ok := m.values[name]; ok {
		if m.p.Values[i].Kind != kind {
			return false, errorsNamed("kind_conflict")
		}
		return false, nil
	}
	id, err := randomID(m.reader)
	if err != nil {
		return false, err
	}
	m.values[name] = len(m.p.Values)
	m.p.Values = append(m.p.Values, Value{ID: id, Name: name, Kind: kind, Slots: []Slot{}})
	m.changed = true
	return true, nil
}
func (m *Mutation) Unset(name string, kind ValueKind, env string) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	if err := m.p.checkEnv(env); err != nil {
		return false, err
	}
	i, ok := m.values[name]
	if !ok {
		return false, nil
	}
	v := &m.p.Values[i]
	if v.Kind != kind {
		return false, errorsNamed("kind_conflict")
	}
	for j, s := range v.Slots {
		if s.Env == env {
			v.Slots = append(v.Slots[:j], v.Slots[j+1:]...)
			m.changed = true
			return true, nil
		}
	}
	return false, nil
}
func (m *Mutation) CreateEnv(env string) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	if !namePattern.MatchString(env) {
		return false, ErrInvalid
	}
	for _, e := range m.p.Envs {
		if e == env {
			return false, nil
		}
	}
	m.p.Envs = append(m.p.Envs, env)
	m.changed = true
	return true, nil
}
func (m *Mutation) RemoveEnv(env string) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	if !namePattern.MatchString(env) {
		return false, ErrInvalid
	}
	if err := m.p.checkEnv(env); err != nil {
		return false, err
	}
	for _, v := range m.p.Values {
		for _, s := range v.Slots {
			if s.Env == env {
				return false, errorsNamed("env_not_empty")
			}
		}
	}
	for i, e := range m.p.Envs {
		if e == env {
			m.p.Envs = append(m.p.Envs[:i], m.p.Envs[i+1:]...)
			m.changed = true
			return true, nil
		}
	}
	return false, nil
}
func (m *Mutation) SetBundle(name string, names []string) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	if !ValidBundleName(name) || len(names) > len(m.p.Values) {
		return false, ErrInvalid
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, n := range names {
		if err := m.check(); err != nil {
			return false, err
		}
		if seen[n] {
			return false, ErrInvalid
		}
		seen[n] = true
		i, ok := m.values[n]
		if !ok {
			return false, errorsNamed("key_not_found")
		}
		ids = append(ids, m.p.Values[i].ID)
	}
	sort.Strings(ids)
	if i, ok := m.bundles[name]; ok {
		old := append([]string{}, m.p.Bundles[i].ValueIDs...)
		sort.Strings(old)
		if reflect.DeepEqual(old, ids) {
			return false, nil
		}
		m.p.Bundles[i].ValueIDs = ids
	} else {
		id, err := randomID(m.reader)
		if err != nil {
			return false, err
		}
		m.bundles[name] = len(m.p.Bundles)
		m.p.Bundles = append(m.p.Bundles, Bundle{ID: id, Name: name, ValueIDs: ids})
	}
	m.changed = true
	return true, nil
}
func (m *Mutation) RenameBundle(name, next string) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	if !ValidBundleName(next) {
		return false, ErrInvalid
	}
	i, ok := m.bundles[name]
	if !ok {
		return false, errorsNamed("bundle_not_found")
	}
	if name == next {
		return false, nil
	}
	if _, exists := m.bundles[next]; exists {
		return false, errorsNamed("bundle_exists")
	}
	m.p.Bundles[i].Name = next
	delete(m.bundles, name)
	m.bundles[next] = i
	m.changed = true
	return true, nil
}
func (m *Mutation) RemoveBundle(name string) (bool, error) {
	if err := m.check(); err != nil {
		return false, err
	}
	i, ok := m.bundles[name]
	if !ok {
		return false, errorsNamed("bundle_not_found")
	}
	m.p.Bundles = append(m.p.Bundles[:i], m.p.Bundles[i+1:]...)
	delete(m.bundles, name)
	for j := i; j < len(m.p.Bundles); j++ {
		m.bundles[m.p.Bundles[j].Name] = j
	}
	m.changed = true
	return true, nil
}

// Seal promotes this public candidate without changing logical identities. The
// caller owns the returned key and must hold the launch mutation lease at publish.
func (m *Mutation) Seal(vaultID string, master *Material) (*Material, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	key, err := m.p.Seal(vaultID, master)
	if err == nil {
		m.key = key
	}
	return key, err
}
