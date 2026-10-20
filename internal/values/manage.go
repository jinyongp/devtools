package values

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

type ManagedItem struct {
	ValueID string `json:"value_id,omitempty"`
	Metadata
	Value *string `json:"value,omitempty"`
}
type ManagedView struct {
	Bundles           []vault.Bundle `json:"bundles"`
	PublicPrefixes    []string       `json:"public_prefixes"`
	IntegrityVerified bool           `json:"integrity_verified"`
	Profile           string         `json:"profile"`
	Env               string         `json:"env"`
	Envs              []string       `json:"envs"`
	Revision          string         `json:"revision"`
	Items             []ManagedItem  `json:"items"`
}
type BundleChange struct {
	Name    string   `json:"name"`
	NewName string   `json:"new_name,omitempty"`
	Members []string `json:"members,omitempty"`
	Preview bool     `json:"preview,omitempty"`
}
type Change struct {
	Bundle    *BundleChange `json:"bundle,omitempty"`
	Import    *BatchImport  `json:"import,omitempty"`
	Action    string        `json:"action"`
	Key       string        `json:"key"`
	Env       string        `json:"env"`
	Value     *string       `json:"value,omitempty"`
	Revision  string        `json:"revision"`
	RequestID string        `json:"request_id"`
}
type ChangeResult struct {
	Bundle     string       `json:"bundle,omitempty"`
	Warnings   []Warning    `json:"warnings,omitempty"`
	Items      []ImportItem `json:"items,omitempty"`
	Applicable bool         `json:"applicable,omitempty"`
	Profile    string       `json:"profile"`
	Changed    bool         `json:"changed"`
	Revision   string       `json:"revision"`
	Replayed   bool         `json:"replayed"`
}
type changeReceipt struct {
	Fingerprint string       `json:"fingerprint"`
	Result      ChangeResult `json:"result"`
	ProfileID   string       `json:"profile_id,omitempty"`
	KeyEpoch    uint64       `json:"key_epoch,omitempty"`
	CreatedAt   int64        `json:"created_at,omitempty"`
	ExpiresAt   int64        `json:"expires_at,omitempty"`
}

func (s Store) managementKey(create bool) ([]byte, error) {
	if _, protected, err := maintenance.ProtectedRootHeld(maintenance.Root(s.Directory)); err != nil || protected {
		return nil, errors.New("legacy values access disabled")
	}
	path := filepath.Join(maintenance.Root(s.Directory), ".maintenance", "values-key")
	b, e := maintenance.Read(path, 32)
	if errors.Is(e, os.ErrNotExist) && create {
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
	return s.inspect(ctx, env, false)
}

func (s Store) inspect(ctx context.Context, env string, exclusive bool) (ManagedView, *protocol.Error) {
	out := ManagedView{Profile: s.Profile, Env: env, Items: []ManagedItem{}}
	if !project.ValidProfile(s.Profile) {
		return out, protocol.NewError("invalid_argument", "Invalid profile.", 2, nil)
	}
	var release func()
	var gateErr error
	if exclusive {
		release, gateErr = maintenance.AcquireExclusive(ctx, maintenance.Root(s.Directory))
	} else {
		release, gateErr = maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	}
	if gateErr != nil {
		return out, gateError(ctx, gateErr)
	}
	if ctx.Err() != nil {
		release()
		return out, protocol.ContextError(ctx.Err())
	}

	_, statErr := os.Lstat(s.Directory)
	if errors.Is(statErr, os.ErrNotExist) {
		if !exclusive {
			release()
			return s.inspect(ctx, env, true)
		}
		if err := privateDirectory(s.Directory); err != nil {
			release()
			return out, storageErrorAt("create_directory", err)
		}
	} else {
		if statErr == nil {
			statErr = platformfs.CheckDirectory(s.Directory)
		}
		if statErr != nil {
			release()
			return out, storageErrorAt("check_directory", statErr)
		}
	}

	key, keyErr := s.managementKey(exclusive)
	if errors.Is(keyErr, os.ErrNotExist) && !exclusive {
		release()
		return s.inspect(ctx, env, true)
	}
	if keyErr != nil {
		release()
		return out, storageErrorAt("read_management_key", keyErr)
	}
	defer release()

	state, err := s.read()
	if err != nil {
		return out, err
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
		return out, gateError(ctx, e)
	}
	defer release()
	if e = privateDirectory(s.Directory); e != nil {
		return out, storageErrorAt("create_directory", e)
	}
	key, e := s.managementKey(true)
	if e != nil {
		return out, storageErrorAt("read_management_key", e)
	}
	fingerprint := signed(key, c)
	receiptPath := s.receiptRelative(c.RequestID)
	previous, found, receiptErr := s.readReceipt(c.RequestID)
	if receiptErr != nil {
		return out, storageErrorAt("read_receipt", receiptErr)
	}
	if found {
		if previous.Fingerprint != fingerprint {
			return out, failure("request_conflict", "Request ID was used with different input.")
		}
		previous.Result.Replayed = true
		return previous.Result, nil
	}
	state, err := s.read()
	if err != nil {
		return out, err
	}
	if signed(key, state) != c.Revision {
		return out, failure("revision_conflict", "Profile changed; reload before editing.")
	}
	if c.Import != nil && c.Action != "import" {
		return out, protocol.NewError("invalid_argument", "Import input requires the import action.", 2, nil)
	}
	switch c.Action {
	case "import":
		if c.Import == nil || c.Key != "" || c.Value != nil {
			return out, protocol.NewError("invalid_argument", "Provide import input and options.", 2, nil)
		}
		if len(c.Import.Content) > 500000 {
			return out, protocol.NewError("invalid_argument", "Use at most 500 KB of .env content.", 2, nil)
		}
		var input map[string]string
		input, err = ParseDotenv(c.Import.Content)
		if err != nil {
			return out, err
		}
		out.Items, out.Applicable, err = state.batchImport(input, c.Import.Variables, c.Env, c.Import.Overwrite, !c.Import.Preview)
		if c.Import.Preview && err == nil {
			out.Revision = c.Revision
			return out, nil
		}
		for _, item := range out.Items {
			if item.Action == "add" || item.Action == "update" {
				out.Changed = true
			}
		}
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
		return out, storageErrorAt("encode_profile", e)
	}
	receipt, e := json.Marshal(changeReceipt{Fingerprint: fingerprint, Result: out})
	if e != nil {
		return out, storageErrorAt("encode_receipt", e)
	}
	if ctx.Err() != nil {
		return out, protocol.ContextError(ctx.Err())
	}
	files, e := profilekey.Replacement(s.Directory, s.Profile, "values", data)
	if e != nil {
		return out, storageErrorAt("prepare_replacement", e)
	}
	files[receiptPath] = receipt
	if e = maintenance.Replace(maintenance.Root(s.Directory), files); e != nil {
		return out, storageErrorAt("replace_profile", e)
	}
	return out, nil
}
