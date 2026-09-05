package values

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type ManagedItem struct {
	Metadata
	Value *string `json:"value,omitempty"`
}
type ManagedView struct {
	Profile  string        `json:"profile"`
	Env      string        `json:"env"`
	Envs     []string      `json:"envs"`
	Revision string        `json:"revision"`
	Items    []ManagedItem `json:"items"`
}
type Change struct {
	Action    string  `json:"action"`
	Key       string  `json:"key"`
	Env       string  `json:"env"`
	Value     *string `json:"value,omitempty"`
	Revision  string  `json:"revision"`
	RequestID string  `json:"request_id"`
}
type ChangeResult struct {
	Profile  string `json:"profile"`
	Changed  bool   `json:"changed"`
	Revision string `json:"revision"`
	Replayed bool   `json:"replayed"`
}
type changeReceipt struct {
	Fingerprint string       `json:"fingerprint"`
	Result      ChangeResult `json:"result"`
}

func (s Store) managementKey() ([]byte, error) {
	path := filepath.Join(maintenance.Root(s.Directory), ".maintenance", "values-key")
	b, e := maintenance.Read(path, 32)
	if errors.Is(e, os.ErrNotExist) {
		b = make([]byte, 32)
		if _, e = rand.Read(b); e == nil {
			e = maintenance.Write(path, b)
		}
	}
	if e == nil && len(b) != 32 {
		e = errors.New("invalid key")
	}
	return b, e
}
func signed(key []byte, v any) string {
	b, _ := json.Marshal(v)
	h := hmac.New(sha256.New, key)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func (s Store) Inspect(ctx context.Context, env string) (ManagedView, *protocol.Error) {
	out := ManagedView{Profile: s.Profile, Env: env, Items: []ManagedItem{}}
	if !project.ValidProfile(s.Profile) {
		return out, protocol.NewError("invalid_argument", "Invalid profile.", 2, nil)
	}
	release, e := maintenance.Acquire(ctx, maintenance.Root(s.Directory))
	if e != nil {
		return out, storageError()
	}
	defer release()
	if e = privateDirectory(s.Directory); e != nil {
		return out, storageError()
	}
	state, err := s.read()
	if err != nil {
		return out, err
	}
	key, e := s.managementKey()
	if e != nil {
		return out, storageError()
	}
	out.Revision = signed(key, state)
	out.Envs = state.EnvNames()
	for _, kind := range []Kind{Variable, Secret} {
		items, err := state.List(kind, env)
		if err != nil {
			return out, err
		}
		for _, m := range items {
			item := ManagedItem{Metadata: m}
			if kind == Variable {
				v, _, _ := state.GetVariable(m.Key, env)
				item.Value = &v
			}
			out.Items = append(out.Items, item)
		}
	}
	return out, nil
}

// Apply shares State mutations with CLI commands and atomically publishes the
// state and a metadata-only retry receipt under the common maintenance lock.
func (s Store) Apply(ctx context.Context, c Change) (ChangeResult, *protocol.Error) {
	out := ChangeResult{Profile: s.Profile}
	if !project.ValidProfile(s.Profile) || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(c.RequestID) || c.Revision == "" {
		return out, protocol.NewError("invalid_argument", "Provide profile, revision and request UUID.", 2, nil)
	}
	release, e := maintenance.Acquire(ctx, maintenance.Root(s.Directory))
	if e != nil {
		return out, storageError()
	}
	defer release()
	if e = privateDirectory(s.Directory); e != nil {
		return out, storageError()
	}
	key, e := s.managementKey()
	if e != nil {
		return out, storageError()
	}
	fingerprint := signed(key, c)
	receiptPath := "backup-receipts/values-" + hex.EncodeToString([]byte(s.Profile)) + "-" + c.RequestID + ".json"
	b, e := maintenance.Read(filepath.Join(maintenance.Root(s.Directory), receiptPath), 1<<20)
	if e == nil {
		var receipt changeReceipt
		if json.Unmarshal(b, &receipt) != nil {
			return out, storageError()
		}
		if receipt.Fingerprint != fingerprint {
			return out, failure("request_conflict", "Request ID was used with different input.")
		}
		receipt.Result.Replayed = true
		return receipt.Result, nil
	}
	if !errors.Is(e, os.ErrNotExist) {
		return out, storageError()
	}
	state, err := s.read()
	if err != nil {
		return out, err
	}
	if signed(key, state) != c.Revision {
		return out, failure("revision_conflict", "Profile changed; reload before editing.")
	}
	switch c.Action {
	case "variable.set", "secret.set":
		if c.Value == nil || c.Key == "" {
			return out, protocol.NewError("invalid_argument", "Provide key and value.", 2, nil)
		}
		kind := Variable
		if c.Action == "secret.set" {
			kind = Secret
		}
		out.Changed, err = state.Set(kind, c.Key, c.Env, *c.Value)
	case "variable.unset", "secret.unset":
		if c.Value != nil || !keyPattern.MatchString(c.Key) {
			return out, protocol.NewError("invalid_argument", "Provide a key for removal.", 2, nil)
		}
		kind := Variable
		if c.Action == "secret.unset" {
			kind = Secret
		}
		out.Changed, err = state.Unset(kind, c.Key, c.Env)
	case "env.create", "env.remove":
		if c.Key != "" || c.Value != nil || !project.ValidProfile(c.Env) {
			return out, protocol.NewError("invalid_argument", "Provide an env name.", 2, nil)
		}
		if c.Action == "env.create" {
			out.Changed, err = state.CreateEnv(c.Env)
		} else {
			out.Changed, err = state.RemoveEnv(c.Env)
		}
	default:
		return out, protocol.NewError("invalid_argument", "Unknown value action.", 2, nil)
	}
	if err != nil {
		return out, err
	}
	if !state.valid(s.Profile) {
		return out, storageError()
	}
	out.Revision = signed(key, state)
	data, e := json.Marshal(state)
	if e != nil {
		return out, storageError()
	}
	receipt, e := json.Marshal(changeReceipt{fingerprint, out})
	if e != nil {
		return out, storageError()
	}
	if ctx.Err() != nil {
		return out, protocol.NewError("canceled", "Request canceled.", 130, nil)
	}
	if e = maintenance.Replace(maintenance.Root(s.Directory), map[string][]byte{"profiles/" + hex.EncodeToString([]byte(s.Profile)) + ".json": data, receiptPath: receipt}); e != nil {
		return out, storageError()
	}
	return out, nil
}
