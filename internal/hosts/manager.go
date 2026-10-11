package hosts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Owner struct {
	Key      string    `json:"key"`
	Mappings []Mapping `json:"mappings"`
}
type state struct {
	Owners  []Owner  `json:"owners"`
	Pending *pending `json:"pending,omitempty"`
}
type pending struct {
	Owners  []Owner `json:"owners"`
	Request Request `json:"request"`
	After   string  `json:"after"`
}
type Manager struct {
	Data     string
	Isolated bool
	Terminal bool
	System   System
	Run      privilege.Runner
	Preview  func([]Mapping)
}

func (m Manager) Status() (map[string]any, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	count := 0
	prepared := 0
	for _, owner := range st.Owners {
		count += len(owner.Mappings)
		for _, mapping := range owner.Mappings {
			if m.Prepared(owner.Key, mapping) {
				prepared++
			}
		}
	}
	return map[string]any{"owners": len(st.Owners), "mappings": count, "prepared": prepared, "pending": st.Pending != nil}, nil
}
func (m Manager) ReferenceDigest() (string, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return "", e
	}
	data, _ := json.Marshal(st)
	return digest(data), nil
}

func (m Manager) read() (state, *protocol.Error) {
	var st state
	e := tasks.ReadPrivate(filepath.Join(m.Data, "hosts", "references.json"), &st)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return st, failure("invalid_storage")
	}
	return st, nil
}
func mappings(owners []Owner) []Mapping {
	out := []Mapping{}
	for _, o := range owners {
		out = append(out, o.Mappings...)
	}
	return out
}
func (m Manager) Prepared(owner string, mapping Mapping) bool {
	st, e := m.read()
	if e != nil || st.Pending != nil {
		return false
	}
	for _, o := range st.Owners {
		if o.Key == owner {
			for _, v := range o.Mappings {
				if v == mapping {
					b, e := m.System.Read()
					if e != nil {
						return false
					}
					for _, line := range strings.Split(string(b), "\n") {
						text, _, _ := strings.Cut(line, "#")
						fields := strings.Fields(text)
						if len(fields) > 1 && fields[0] == mapping.Address {
							for _, host := range fields[1:] {
								if host == mapping.Host {
									return true
								}
							}
						}
					}
					return false
				}
			}
		}
	}
	return false
}
func (m Manager) OwnerMappings(owner string) ([]Mapping, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	for _, o := range st.Owners {
		if o.Key == owner {
			return append([]Mapping{}, o.Mappings...), nil
		}
	}
	return nil, nil
}
func (m Manager) RemoveOtherOwners(ctx context.Context, prefix, keep string) *protocol.Error {
	st, e := m.read()
	if e != nil {
		return e
	}
	for _, owner := range st.Owners {
		if owner.Key != keep && strings.HasPrefix(owner.Key, prefix) {
			if e := m.Sync(ctx, owner.Key, nil, ""); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m Manager) RemovePrefix(ctx context.Context, prefix string) *protocol.Error {
	st, e := m.read()
	if e != nil {
		return e
	}
	owners := append([]Owner{}, st.Owners...)
	if st.Pending != nil {
		owners = append(owners, st.Pending.Owners...)
	}
	seen := map[string]bool{}
	for _, owner := range owners {
		if strings.HasPrefix(owner.Key, prefix) && !seen[owner.Key] {
			seen[owner.Key] = true
			if e := m.Sync(ctx, owner.Key, nil, ""); e != nil {
				return e
			}
		}
	}
	return nil
}
func (m Manager) PreviewChange(owner string, desired []Mapping) (map[string]any, *protocol.Error) {
	before, e := m.System.Read()
	if e != nil {
		return nil, e
	}
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	if st.Pending != nil {
		return nil, failure("recovery_required")
	}
	if e := validateOwnership(before, mappings(st.Owners)); e != nil {
		return nil, e
	}
	owners := replaceOwner(st.Owners, owner, desired)
	after, e := Render(before, mappings(owners))
	if e != nil {
		return nil, e
	}
	return map[string]any{"digest": digest(before), "changed": !bytes.Equal(before, after), "mappings": desired}, nil
}
func replaceOwner(owners []Owner, key string, desired []Mapping) []Owner {
	out := []Owner{}
	for _, o := range owners {
		if o.Key != key {
			out = append(out, o)
		}
	}
	if len(desired) > 0 {
		out = append(out, Owner{key, desired})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Sync keeps a pending journal before the OS helper so a lost response can
// reconcile the exact mapping without repeating an unsafe write.
func (m Manager) Sync(ctx context.Context, owner string, desired []Mapping, expected string) *protocol.Error {
	if owner == "" {
		return failure("invalid_argument")
	}
	if m.Isolated && len(desired) > 0 {
		return netops.Failure("isolated_operation_not_allowed", "System hosts changes are unavailable in isolated mode; use .localhost or external DNS.")
	}
	root := filepath.Join(m.Data, "hosts")
	if tasks.PrivateDir(root) != nil {
		return failure("io_error")
	}
	unlock, err := tasks.Lock(ctx, filepath.Join(root, "references.lock"))
	if err != nil {
		return failure("canceled")
	}
	defer unlock()
	st, e := m.read()
	if e != nil {
		return e
	}
	before, e := m.System.Read()
	if e != nil {
		return e
	}
	if st.Pending != nil {
		p := st.Pending
		if digest(before) == p.After {
			st.Owners = p.Owners
			st.Pending = nil
			if tasks.WritePrivate(filepath.Join(root, "references.json"), st) != nil {
				return failure("io_error")
			}
		} else if digest(before) != p.Request.Expected {
			return failure("recovery_required")
		} else {
			if e := m.apply(ctx, p.Request); e != nil {
				return e
			}
			st.Owners = p.Owners
			st.Pending = nil
			if tasks.WritePrivate(filepath.Join(root, "references.json"), st) != nil {
				return failure("io_error")
			}
		}
		before, e = m.System.Read()
		if e != nil {
			return e
		}
	}
	if expected != "" && expected != digest(before) {
		return failure("stale_preview")
	}
	if e := validateOwnership(before, mappings(st.Owners)); e != nil {
		return e
	}
	owners := replaceOwner(st.Owners, owner, desired)
	after, e := Render(before, mappings(owners))
	if e != nil {
		return e
	}
	if bytes.Equal(before, after) {
		st.Owners = owners
		return writeState(root, st)
	}
	if m.Isolated {
		return netops.Failure("isolated_operation_not_allowed", "System hosts changes are unavailable in isolated mode.")
	}
	q := Request{Expected: digest(before), Mappings: mappings(owners)}
	st.Pending = &pending{owners, q, digest(after)}
	if e := writeState(root, st); e != nil {
		return e
	}
	if m.Preview != nil {
		m.Preview(q.Mappings)
	}
	if e := m.apply(ctx, q); e != nil {
		return e
	}
	current, e := m.System.Read()
	if e != nil {
		return e
	}
	if digest(current) != digest(after) {
		return failure("recovery_required")
	}
	st.Owners = owners
	st.Pending = nil
	return writeState(root, st)
}
func writeState(root string, st state) *protocol.Error {
	if tasks.WritePrivate(filepath.Join(root, "references.json"), st) != nil {
		return failure("io_error")
	}
	return nil
}
func (m Manager) apply(ctx context.Context, q Request) *protocol.Error {
	run := m.Run
	if run == nil {
		run = privilege.Run
	}
	return run(ctx, "hosts", q, m.Terminal)
}
