package values

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/vault"
)

var modernRequestID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func editRevision(profile string, p *vault.Profile, exists bool) string {
	source := "absent/" + profile
	if exists {
		source = p.ID + "/" + p.Generation + "/" + strconv.FormatUint(p.Revision, 10) + "/" + strconv.FormatUint(p.KeyEpoch, 10) + "/" + hex.EncodeToString(p.Tag)
	}
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
func (s ModernStore) Inspect(ctx context.Context, env string) (ManagedView, *protocol.Error) {
	out := ManagedView{Profile: s.Profile, Env: env, Items: []ManagedItem{}, Bundles: []vault.Bundle{}, PublicPrefixes: FrontendPublicPrefixes()}
	if !project.ValidProfile(s.Profile) {
		return out, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, nil)
	}
	p, exists, needsExclusive, e := s.inspectionProfile(ctx, false)
	if e != nil {
		return out, e
	}
	if needsExclusive {
		p, exists, _, e = s.inspectionProfile(ctx, true)
		if e != nil {
			return out, e
		}
	}
	state, err := metadataState(p)
	if err != nil {
		return out, VaultError(ctx, err)
	}
	out.Bundles = append([]vault.Bundle{}, p.Bundles...)
	ids := map[string]string{}
	for _, v := range p.Values {
		ids[v.Name] = v.ID
	}
	out.Revision = editRevision(s.Profile, p, exists)
	out.Envs = state.EnvNames()
	for _, kind := range []Kind{Variable, Secret} {
		entries, e := state.List(kind, env)
		if e != nil {
			return out, e
		}
		for _, metadata := range entries {
			item := ManagedItem{Metadata: metadata, ValueID: ids[metadata.Key]}
			if kind == Variable {
				if value, _, e := state.GetVariable(metadata.Key, env); e == nil {
					item.Value = &value
				}
			}
			out.Items = append(out.Items, item)
		}
	}
	return out, nil
}

// inspectionProfile releases the shared gate before retrying a legacy conversion.
// Every retry rereads storage, so a concurrent writer or initialization wins.
func (s ModernStore) inspectionProfile(ctx context.Context, exclusive bool) (*vault.Profile, bool, bool, *protocol.Error) {
	acquire := maintenance.AcquireShared
	if exclusive {
		acquire = maintenance.AcquireExclusive
	}
	release, err := acquire(ctx, s.root())
	if err != nil {
		return nil, false, false, VaultError(ctx, err)
	}
	defer release()
	raw, err := profilekey.Snapshot(s.Directory, s.Profile, "values", vault.MaxDocumentBytes)
	if err != nil {
		return nil, false, false, storageError()
	}
	defer clear(raw)
	var p *vault.Profile
	exists := raw != nil
	if exists {
		p, err = vault.DecodeProfile(raw)
		if err != nil {
			legacy, legacyErr := InspectSnapshot(raw, s.Profile)
			if legacyErr != nil || hasSecret(legacy) {
				return nil, false, false, failure("migration_required", "Migrate the legacy profile before managed changes.")
			}
			if _, protected, e := maintenance.ProtectedRootHeld(s.root()); e != nil || protected {
				return nil, false, false, failure("migration_required", "Migrate the legacy profile before managed changes.")
			}
			if !exclusive {
				return nil, false, true, nil
			}
			p, err = convertLegacy(legacy, "", nil)
			if err != nil {
				return nil, false, false, VaultError(ctx, err)
			}
			if err = s.publishHeld(ctx, p, nil, nil, ""); err != nil {
				return nil, false, false, VaultError(ctx, err)
			}
		}
	} else {
		p, err = vault.NewProfile(s.Profile)
		if err != nil {
			return nil, false, false, VaultError(ctx, err)
		}
	}
	return p, exists, false, nil
}
func sharedReceiptKey(ctx context.Context, root, vaultID string, master *vault.Material, create bool) (*vault.Material, map[string][]byte, error) {
	if master == nil {
		raw, err := (Store{Directory: filepath.Join(root, "profiles")}).managementKey(create)
		if err != nil {
			return nil, nil, err
		}
		defer clear(raw)
		return vault.NewMaterial(raw), nil, nil
	}
	raw, err := maintenance.Read(filepath.Join(root, "vault", "legacy-verification.json"), vault.MaxDocumentBytes)
	if err == nil {
		key, err := vault.OpenProtected(ctx, master, vaultID, vaultID, vault.MaterialLegacyVerification, raw)
		if err != nil {
			return nil, nil, err
		}
		bytes := key.CopyBytes()
		valid := len(bytes) == 32
		clear(bytes)
		if !valid {
			key.Destroy()
			return nil, nil, vault.ErrInvalid
		}
		return key, nil, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, vault.ErrRecovery
	}
	if !create {
		return nil, nil, vault.ErrKeyMissing
	}
	key, err := vault.NewKey()
	if err != nil {
		return nil, nil, err
	}
	encrypted, err := vault.SealProtected(ctx, master, vaultID, vaultID, vault.MaterialLegacyVerification, key)
	if err != nil {
		key.Destroy()
		return nil, nil, err
	}
	return key, map[string][]byte{"vault/legacy-verification.json": encrypted}, nil
}
func requestMAC(key *vault.Material, c Change) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", vault.ErrInvalid
	}
	defer clear(raw)
	bytes := key.CopyBytes()
	defer clear(bytes)
	if len(bytes) != 32 {
		return "", vault.ErrKeyMissing
	}
	mac := hmac.New(sha256.New, bytes)
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil)), nil
}
func (s ModernStore) receiptHeld(ctx context.Context, master *vault.Material, vaultID, id string) (vault.ValueReceipt, ChangeResult, bool, error) {
	legacy := Store{Directory: s.Directory, Profile: s.Profile}
	paths := []string{legacy.receiptRelative(id), "backup-receipts/values-" + hex.EncodeToString([]byte(s.Profile)) + "-" + id + ".json"}
	var receipt vault.ValueReceipt
	var result ChangeResult
	found := false
	store := vault.FileStore{Root: s.root(), VaultID: vaultID, CatalogKey: master}
	scope := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	for index, path := range paths {
		if index == 1 && len(filepath.Base(path)) > 255 {
			continue
		}
		candidate, present, err := store.ReadValueReceiptHeld(ctx, path, scope)
		if err != nil {
			return receipt, result, false, err
		}
		if !present {
			continue
		}
		var value ChangeResult
		if vault.StrictDecode(candidate.Result, &value) != nil || value.Profile != s.Profile {
			return receipt, result, false, vault.ErrInvalid
		}
		if found && (candidate.Fingerprint != receipt.Fingerprint || candidate.ProfileID != receipt.ProfileID || candidate.KeyEpoch != receipt.KeyEpoch || !reflect.DeepEqual(value, result)) {
			return receipt, result, false, vault.ErrIntegrity
		}
		receipt, result, found = candidate, value, true
	}
	return receipt, result, found, nil
}
func mutateChange(state *Editor, c Change) (ChangeResult, *protocol.Error) {
	out := ChangeResult{Profile: state.Profile}
	if c.Import != nil && c.Action != "import" {
		return out, protocol.NewError("invalid_argument", "Import input requires the import action.", 2, nil)
	}
	var err *protocol.Error
	switch c.Action {
	case "import":
		if c.Import == nil || c.Key != "" || c.Value != nil || len(c.Import.Content) > 500000 || (c.Import.Bundle != "" && !vault.ValidBundleName(c.Import.Bundle)) {
			return out, protocol.NewError("invalid_argument", "Provide bounded import input and options.", 2, nil)
		}
		out.Bundle = c.Import.Bundle
		input, e := ParseDotenv(c.Import.Content)
		if e != nil {
			return out, e
		}
		if c.Import.LayerOnly {
			out.Items, out.Applicable, err = state.PlanImport(input, c.Import.Variables, c.Env, c.Import.Overwrite)
			if err == nil && !c.Import.Preview {
				out.Items, _, err = state.Import(input, c.Import.Variables, c.Env, c.Import.Overwrite)
			}
		} else {
			out.Items, out.Applicable, err = state.batchImport(input, c.Import.Variables, c.Env, c.Import.Overwrite, !c.Import.Preview)
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
	if err == nil {
		if c.Action == "secret.set" {
			if warning := FrontendSecretWarning(c.Key); warning != nil {
				out.Warnings = []Warning{*warning}
			}
		}
		if c.Action == "import" {
			out.Warnings = ImportWarnings(out.Items)
		}
	}
	return out, err
}

func (s ModernStore) replayHeld(ctx context.Context, c Change, p *vault.Profile, key, master *vault.Material, vaultID string) (ChangeResult, bool, *protocol.Error) {
	out := ChangeResult{Profile: s.Profile}
	previous, result, found, err := s.receiptHeld(ctx, master, vaultID, c.RequestID)
	if err != nil {
		return out, false, VaultError(ctx, err)
	}
	if !found {
		return out, false, nil
	}

	if previous.ExpiresAt != 0 && time.Now().Unix() >= previous.ExpiresAt {
		return out, true, failure("request_expired", "The replay window has expired; use a new request UUID.")
	}
	var verify *vault.Material
	if previous.KeyEpoch == 0 {
		verify, _, err = sharedReceiptKey(ctx, s.root(), vaultID, master, false)
	} else if previous.ProfileID == p.ID && previous.KeyEpoch == p.KeyEpoch {
		verify, err = vault.ReceiptKey(key, vaultID, p.ID)
	} else {
		verify, err = (vault.FileStore{Root: s.root(), VaultID: vaultID, CatalogKey: master}).HistoricalReceiptKeyHeld(ctx, previous.ProfileID, previous.KeyEpoch)
	}
	if err != nil {
		return out, true, VaultError(ctx, err)
	}
	defer verify.Destroy()
	fingerprint, e := requestMAC(verify, c)
	if e != nil {
		return out, true, VaultError(ctx, e)
	}
	if !hmac.Equal([]byte(fingerprint), []byte(previous.Fingerprint)) {
		return out, true, failure("request_conflict", "Request ID was used with different input.")
	}
	result.Replayed = true
	return result, true, nil
}

func (s ModernStore) Apply(ctx context.Context, c Change) (ChangeResult, *protocol.Error) {
	out := ChangeResult{Profile: s.Profile}
	if !project.ValidProfile(s.Profile) || !modernRequestID.MatchString(c.RequestID) || c.Revision == "" {
		return out, protocol.NewError("invalid_argument", "Provide profile, revision and request UUID.", 2, nil)
	}
	master, vaultID, validate, err := s.writeScope(ctx)
	if err != nil {
		return out, VaultError(ctx, err)
	}
	defer master.Destroy()
	p, key, base, err := s.readForEdit(ctx, master, vaultID)
	if err != nil {
		return out, VaultError(ctx, err)
	}
	defer key.Destroy()
	// Receipt reads share one gate with the original profile used for replay.
	releaseRead, err := maintenance.AcquireShared(ctx, s.root())
	if err != nil {
		return out, VaultError(ctx, err)
	}
	current, currentKey, currentExists, err := s.readHeld(ctx, master, vaultID)
	if err != nil {
		releaseRead()
		return out, VaultError(ctx, err)
	}
	replay, found, replayErr := s.replayHeld(ctx, c, current, currentKey, master, vaultID)
	currentKey.Destroy()
	releaseRead()
	if replayErr != nil {
		return out, replayErr
	}
	if found {
		return replay, nil
	}
	if e := s.checkBase(base, current, currentExists); e != nil {
		return out, e
	}
	if c.Revision != editRevision(s.Profile, p, base.exists) {
		return out, failure("revision_conflict", "Profile changed; reload before editing.")
	}
	var editor *Editor
	defer func() {
		if editor != nil && editor.promoted {
			editor.key.Destroy()
		}
	}()
	stopPrepare := s.measure("prepare")
	_, err = p.Batch(ctx, key, func(m *vault.Mutation) error {
		editor = newEditor(ctx, p, m, key, master, vaultID)
		if c.Bundle != nil {
			changed, err := editor.bundle(c)
			if err != nil {
				return err
			}
			out.Changed, out.Bundle = changed, c.Bundle.Name
		} else {
			var e *protocol.Error
			out, e = mutateChange(editor, c)
			if e != nil {
				return e
			}
		}
		if c.Import != nil && c.Import.Bundle != "" && !c.Import.Preview {
			changed, err := editor.importBundle(c.Import.Bundle, out.Items)
			if err != nil {
				return err
			}
			out.Changed = out.Changed || changed
		}
		return nil
	})
	stopPrepare()
	if err != nil {
		if e, ok := err.(*protocol.Error); ok {
			return out, e
		}
		return out, VaultError(ctx, err)
	}
	if c.Bundle != nil && c.Bundle.Preview || c.Import != nil && c.Import.Preview {
		out.Changed = false
		out.Revision = c.Revision
		return out, nil
	}
	if editor.promoted {
		key = editor.key
	}
	if s.beforeCommit != nil {
		s.beforeCommit()
	}
	release, err := s.lockCommit(ctx, base, editor.promoted, validate)
	if err != nil {
		return out, VaultError(ctx, err)
	}
	defer release()
	current, currentKey, currentExists, err = s.readHeld(ctx, master, vaultID)
	if err != nil {
		return out, VaultError(ctx, err)
	}
	replay, found, replayErr = s.replayHeld(ctx, c, current, currentKey, master, vaultID)
	currentKey.Destroy()
	if replayErr != nil {
		return out, replayErr
	}
	if found {
		return replay, nil
	}
	if e := s.checkBase(base, current, currentExists); e != nil {
		return out, e
	}
	out.Revision = editRevision(s.Profile, p, true)
	var fingerprintKey *vault.Material
	extra := map[string][]byte{}
	if p.Sealed {
		fingerprintKey, err = vault.ReceiptKey(key, vaultID, p.ID)
	} else {
		fingerprintKey, extra, err = sharedReceiptKey(ctx, s.root(), vaultID, master, true)
	}
	if err != nil {
		return out, VaultError(ctx, err)
	}
	defer fingerprintKey.Destroy()
	fingerprint, err := requestMAC(fingerprintKey, c)
	if err != nil {
		return out, VaultError(ctx, err)
	}
	outcome, err := json.Marshal(out)
	if err != nil {
		return out, storageError()
	}
	now := time.Now()
	receipt := vault.ValueReceipt{Fingerprint: fingerprint, Result: outcome, ProfileID: p.ID, KeyEpoch: p.KeyEpoch, CreatedAt: now.Unix(), ExpiresAt: now.Add(retention.ValueReceiptAge).Unix()}
	body, err := json.Marshal(receipt)
	if err != nil {
		return out, storageError()
	}
	if master != nil {
		plain := vault.NewMaterial(body)
		clear(body)
		body, err = vault.SealProtected(ctx, master, vaultID, strings.ToLower(strings.ReplaceAll(c.RequestID, "-", "")), vault.MaterialValueReceipt, plain)
		plain.Destroy()
		if err != nil {
			return out, VaultError(ctx, err)
		}
	}
	if extra == nil {
		extra = map[string][]byte{}
	}
	if master != nil {
		retired, e := (vault.FileStore{Root: s.root(), VaultID: vaultID, CatalogKey: master}).PruneReceiptKeysHeld(ctx, time.Now())
		if e != nil {
			return out, VaultError(ctx, e)
		}
		for path, data := range retired {
			extra[path] = data
		}
	}
	extra[(Store{Directory: s.Directory, Profile: s.Profile}).receiptRelative(c.RequestID)] = body
	if master != nil {
		err = (vault.FileStore{Root: s.root(), VaultID: vaultID, CatalogKey: master}).PublishManagedWithFilesHeld(ctx, p, key, extra)
	} else {
		raw, e := p.Encode()
		if e != nil {
			return out, VaultError(ctx, e)
		}
		files, e := profilekey.Replacement(s.Directory, s.Profile, "values", raw)
		if e != nil {
			return out, storageError()
		}
		for path, body := range extra {
			files[path] = body
		}
		if e = vault.AddCommitHint(files, p); e != nil {
			return out, VaultError(ctx, e)
		}
		err = maintenance.Replace(s.root(), files)
	}
	if err != nil {
		return out, VaultError(ctx, err)
	}
	return out, nil
}
