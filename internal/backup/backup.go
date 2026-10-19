// Package backup provides encrypted snapshots and recoverable profile restoration.
package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	profilecatalog "github.com/jinyongp/devtools/internal/profiles"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

const limit int64 = 128 << 20

// Import receipts include the complete metadata diff. Keep reads and writes
// bounded by the same limit so every committed result can be replayed.
const restoreReceiptLimit int64 = 128 << 20

type Engine struct {
	Data, Config, Cache string
	// VaultMaster is called once per operation, before all disk/launch gates.
	// Nil preserves the legacy reader/writer for roots not yet migrated.
	VaultMaster func(context.Context) (*vault.Material, error)
	vaultMaster *vault.Material
	vaultID     string
}
type Config struct {
	Directory string `json:"directory"`
	Recipient string `json:"recipient"`
}
type Status struct {
	Configured bool   `json:"configured"`
	Directory  string `json:"directory"`
	Recipient  string `json:"recipient"`
}
type Snapshot struct {
	Version  int       `json:"version"`
	Created  string    `json:"created_at"`
	Profiles []Profile `json:"profiles"`
}
type Profile struct {
	Name   string          `json:"name"`
	Values json.RawMessage `json:"values,omitempty"`
	Tasks  json.RawMessage `json:"tasks,omitempty"`
}
type Summary struct {
	Profile string `json:"profile"`
	Values  bool   `json:"values"`
	Tasks   bool   `json:"tasks"`
}
type Metadata struct {
	CreatedAt string    `json:"created_at"`
	Profiles  []Summary `json:"profiles"`
}
type CreateResult struct {
	Path      string    `json:"path"`
	Profiles  []Summary `json:"profiles"`
	CreatedAt string    `json:"created_at"`
}
type CreateOptions struct {
	Profile       string
	Output        string
	RecipientPath string
	Recipient     string
	Passphrase    string
}
type Plan struct {
	Replayed     bool     `json:"-"` // Response metadata; never stored in the restore receipt.
	Digest       string   `json:"digest"`
	Targets      []Target `json:"targets"`
	Applied      bool     `json:"applied"`
	SafetyBackup string   `json:"safety_backup,omitempty"`
	importData   *importReceiptData
}
type ImportRequest struct {
	Path              string
	IdentityPath      string
	Passphrase        string
	RecoveryDirectory string
	Source            string
	Target            string
	Expected          string
	RequestID         string
	Replace           bool
}
type ImportResult struct {
	Plan   Plan
	Source Summary
	Target string
	Diff   profilecatalog.Diff
}
type importReceiptData struct {
	Source Summary             `json:"source"`
	Target string              `json:"target"`
	Diff   profilecatalog.Diff `json:"diff"`
}
type importContext struct {
	Source    Summary
	Canonical profilecatalog.Canonical
}
type restoreReceipt struct {
	Fingerprint          string             `json:"fingerprint"`
	OperationFingerprint string             `json:"operation_fingerprint,omitempty"`
	Plan                 Plan               `json:"plan"`
	Import               *importReceiptData `json:"import,omitempty"`
}
type restoreFingerprintInput struct {
	Cipher   []byte
	Source   string
	Target   string
	Expected string
	Replace  bool
}
type recoveryOptions struct {
	Directory  string
	Recipient  string
	Passphrase string
	Prefix     string
}

type restoreRequest struct {
	Snapshot             Snapshot
	Cipher               []byte
	Source               string
	Target               string
	Expected             string
	RequestID            string
	OperationFingerprint string
	Replace              bool
	Import               *importContext
	Recovery             *recoveryOptions
}
type Target struct {
	Source  string `json:"source"`
	Profile string `json:"profile"`
	Exists  bool   `json:"exists"`
}

var restoreRequestIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func restoreFingerprint(input restoreFingerprintInput) string {
	mode := "create"
	if input.Replace {
		mode = "replace"
	}
	return digest([]byte(digest(input.Cipher) + "\n" + input.Source + "\n" + input.Target + "\n" + input.Expected + "\n" + mode))
}

func failure(code string) *protocol.Error {
	exit := 3
	switch code {
	case "invalid_argument":
		exit = 2
	case "storage_error":
		exit = 1
	case "canceled":
		exit = 130
	}
	return protocol.NewError(code, "Backup operation could not be completed; check the selected files and target state.", exit, nil)
}

func outputExistsFailure() *protocol.Error {
	return protocol.NewError("backup_error", "Backup operation could not be completed; check the selected files and target state.", 3, map[string]any{"cause": "output_exists"})
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func file(domain, profile string) string {
	return profilekey.StateRelative(domain, profile)
}

func exclusive(path string, b []byte) error {
	return exclusiveContext(context.Background(), path, b)
}

func Keygen(identityPath, recipientPath string) (any, *protocol.Error) {
	if filepath.Clean(identityPath) == filepath.Clean(recipientPath) {
		return nil, failure("invalid_argument")
	}
	id, e := age.GenerateX25519Identity()
	if e != nil {
		return nil, failure("backup_error")
	}
	if e = exclusive(identityPath, []byte(id.String()+"\n")); e != nil {
		return nil, failure("backup_error")
	}
	if e = exclusive(recipientPath, []byte(id.Recipient().String()+"\n")); e != nil {
		_ = os.Remove(identityPath)
		return nil, failure("backup_error")
	}
	return map[string]string{"identity_file": identityPath, "recipient_file": recipientPath}, nil
}
func canonicalRecipient(value string) (string, error) {
	r, err := age.ParseX25519Recipient(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return r.String(), nil
}
func recipient(path string) (string, error) {
	b, err := platformfs.ReadExternalPrivate(path, 4096)
	if err != nil {
		return "", err
	}
	return canonicalRecipient(string(b))
}
func (e Engine) Configure(directory, recipientPath string) (any, *protocol.Error) {
	d, err := filepath.Abs(directory)
	if err != nil {
		return nil, failure("invalid_argument")
	}
	r, err := recipient(recipientPath)
	if err != nil {
		return nil, failure("invalid_recipient")
	}
	if err = ensureConfigDirectory(e.Config); err != nil {
		return nil, failure("backup_error")
	}
	c := Config{d, r}
	b, _ := json.Marshal(c)
	if err = maintenance.Write(filepath.Join(e.Config, "backup.json"), b); err != nil {
		return nil, failure("backup_error")
	}
	return c, nil
}
func (e Engine) config() (Config, error) {
	var c Config
	b, err := maintenance.Read(filepath.Join(e.Config, "backup.json"), 4096)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err == nil && !filepath.IsAbs(c.Directory) {
		err = errors.New("invalid backup directory")
	}
	if err == nil {
		c.Recipient, err = canonicalRecipient(c.Recipient)
	}
	return c, err
}
func (e Engine) Status() (Status, *protocol.Error) {
	c, err := e.config()
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, failure("storage_error")
	}
	return Status{Configured: true, Directory: c.Directory, Recipient: c.Recipient}, nil
}

func logicalDomain(directory string) string {
	if directory == "profiles" {
		return "values"
	}
	return "tasks"
}

func (e Engine) snapshot(selected string) (Snapshot, error) {
	s := Snapshot{Version: 1, Created: time.Now().UTC().Format(time.RFC3339Nano), Profiles: []Profile{}}
	valid := false
	defer func() {
		if !valid {
			destroySnapshot(&s)
		}
	}()
	names := map[string]bool{}
	if selected != "" {
		names[selected] = true
	} else {
		for _, domain := range []string{"profiles", "tasks"} {
			items, err := profilekey.Enumerate(filepath.Join(e.Data, domain), logicalDomain(domain))
			if err != nil {
				return s, err
			}
			for _, name := range items {
				names[name] = true
			}
		}
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		p := Profile{Name: name}
		valuesData, err := e.valueSnapshotHeld(context.Background(), name)
		if err != nil {
			return s, err
		}
		if valuesData != nil {
			checked, err := values.RestoreSnapshot(valuesData, name, name)
			clear(checked)
			if err != nil {
				clear(valuesData)
				return s, err
			}
			p.Values = valuesData
		}
		taskSnapshot, err := (tasks.Store{Directory: filepath.Join(e.Data, "tasks"), Profile: name}).ExportSnapshotHeld(limit)
		if err != nil {
			clear(p.Values)
			return s, err
		}
		if taskSnapshot.Data != nil {
			// ExportSnapshotHeld already validates and canonicalizes the logical
			// journal, so replaying the same history here would duplicate export work.
			p.Tasks = taskSnapshot.Data
		}
		if p.Values == nil && p.Tasks == nil {
			return s, os.ErrNotExist
		}
		s.Profiles = append(s.Profiles, p)
	}
	valid = true
	return s, nil
}
func encodeTo(s Snapshot, recipient age.Recipient) ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("backup too large")
	}
	defer clear(b)
	var out bytes.Buffer
	w, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, err
	}
	if _, err = w.Write(b); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func encode(s Snapshot, r string) ([]byte, error) {
	rec, err := age.ParseX25519Recipient(r)
	if err != nil {
		return nil, err
	}
	return encodeTo(s, rec)
}

func encodePassphrase(s Snapshot, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("empty passphrase")
	}
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, err
	}
	return encodeTo(s, recipient)
}
func summaries(s Snapshot) []Summary {
	out := []Summary{}
	for _, p := range s.Profiles {
		out = append(out, Summary{p.Name, p.Values != nil, p.Tasks != nil})
	}
	return out
}
func (e Engine) Create(ctx context.Context, profile, output, recipientPath string) (CreateResult, *protocol.Error) {
	return e.CreateWithRecipient(ctx, CreateOptions{Profile: profile, Output: output, RecipientPath: recipientPath})
}
func (e Engine) CreateWithRecipient(ctx context.Context, request CreateOptions) (CreateResult, *protocol.Error) {
	if request.Profile != "" && !project.ValidProfile(request.Profile) || request.RecipientPath != "" && request.Recipient != "" {
		return CreateResult{}, failure("invalid_argument")
	}
	var c Config
	configLoaded := false
	loadConfig := func() *protocol.Error {
		if configLoaded {
			return nil
		}
		configured, err := e.config()
		if err != nil {
			return failure("backup_not_configured")
		}
		c.Directory = configured.Directory
		if c.Recipient == "" {
			c.Recipient = configured.Recipient
		}
		configLoaded = true
		return nil
	}
	switch {
	case request.RecipientPath != "":
		r, err := recipient(request.RecipientPath)
		if err != nil {
			return CreateResult{}, failure("invalid_recipient")
		}
		c.Recipient = r
	case request.Recipient != "":
		r, err := canonicalRecipient(request.Recipient)
		if err != nil {
			return CreateResult{}, failure("invalid_recipient")
		}
		c.Recipient = r
	default:
		if failure := loadConfig(); failure != nil {
			return CreateResult{}, failure
		}
	}
	output := request.Output
	if output == "" {
		if failure := loadConfig(); failure != nil {
			return CreateResult{}, failure
		}
		if platformfs.EnsureExternalDirectory(c.Directory) != nil {
			return CreateResult{}, failure("backup_error")
		}
		output = filepath.Join(c.Directory, "devtools-"+tasks.ID()+".age")
	}
	return e.createArchive(ctx, request.Profile, output, func(snapshot Snapshot) ([]byte, error) {
		return encode(snapshot, c.Recipient)
	})
}

func (e Engine) CreateWithPassphrase(ctx context.Context, request CreateOptions) (CreateResult, *protocol.Error) {
	if request.Profile != "" && !project.ValidProfile(request.Profile) || request.Output == "" || request.Passphrase == "" || request.Recipient != "" || request.RecipientPath != "" {
		return CreateResult{}, failure("invalid_argument")
	}
	return e.createArchive(ctx, request.Profile, request.Output, func(snapshot Snapshot) ([]byte, error) {
		return encodePassphrase(snapshot, request.Passphrase)
	})
}

func (e Engine) createArchive(ctx context.Context, profile, output string, encoder func(Snapshot) ([]byte, error)) (CreateResult, *protocol.Error) {
	scoped, closeScope, scopeErr := e.withVault(ctx)
	if scopeErr != nil {
		return CreateResult{}, scopeErr
	}
	defer closeScope()
	e = scoped
	release, err := maintenance.Acquire(ctx, e.Data)
	if err != nil {
		return CreateResult{}, failure("storage_error")
	}
	defer release()
	if err := e.checkVaultRootHeld(); err != nil {
		return CreateResult{}, values.VaultError(ctx, err)
	}
	s, err := e.snapshot(profile)
	if err != nil {
		return CreateResult{}, failure("backup_error")
	}
	defer destroySnapshot(&s)
	if ctx.Err() != nil {
		return CreateResult{}, failure("canceled")
	}
	b, err := encoder(s)
	if err != nil {
		return CreateResult{}, failure("backup_error")
	}
	if err := exclusiveContext(ctx, output, b); err != nil {
		if ctx.Err() != nil {
			return CreateResult{}, failure("canceled")
		}
		if errors.Is(err, os.ErrExist) {
			return CreateResult{}, outputExistsFailure()
		}
		return CreateResult{}, failure("backup_error")
	}
	return CreateResult{Path: output, Profiles: summaries(s), CreatedAt: s.Created}, nil
}

func decodeWithIdentity(path string, identity age.Identity) (Snapshot, []byte, error) {
	var snapshot Snapshot
	valid := false
	defer func() {
		if !valid {
			destroySnapshot(&snapshot)
		}
	}()
	cipher, err := platformfs.ReadExternalPrivate(path, limit+1<<20)
	if err != nil {
		return snapshot, nil, err
	}
	reader, err := age.Decrypt(bytes.NewReader(cipher), identity)
	if err != nil {
		return snapshot, nil, err
	}
	// Read to authenticated EOF before any parsing or mutation.
	plain, err := io.ReadAll(io.LimitReader(reader, limit+1))
	defer clear(plain)
	if err != nil || int64(len(plain)) > limit {
		return snapshot, nil, errors.New("invalid encrypted backup")
	}
	d := json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if d.Decode(&snapshot) != nil || d.Decode(new(any)) != io.EOF || snapshot.Version != 1 {
		return snapshot, nil, errors.New("invalid backup")
	}
	seen := map[string]bool{}
	for _, p := range snapshot.Profiles {
		if !project.ValidProfile(p.Name) || seen[p.Name] || p.Values == nil && p.Tasks == nil {
			return snapshot, nil, errors.New("invalid profile")
		}
		seen[p.Name] = true
		if p.Values != nil {
			checked, checkErr := values.RestoreSnapshot(p.Values, p.Name, p.Name)
			clear(checked)
			if checkErr != nil {
				return snapshot, nil, checkErr
			}
		}
		if p.Tasks != nil {
			checked, checkErr := tasks.RestoreSnapshot(p.Tasks, p.Name, p.Name)
			clear(checked)
			if checkErr != nil {
				return snapshot, nil, checkErr
			}
		}
	}
	valid = true
	return snapshot, cipher, nil
}

func decode(path, identityPath string) (Snapshot, []byte, string, error) {
	var snapshot Snapshot
	key, err := platformfs.ReadExternalPrivate(identityPath, 4096)
	defer clear(key)
	if err != nil {
		return snapshot, nil, "", err
	}
	identity, err := age.ParseX25519Identity(strings.TrimSpace(string(key)))
	if err != nil {
		return snapshot, nil, "", err
	}
	snapshot, cipher, err := decodeWithIdentity(path, identity)
	if err != nil {
		return snapshot, nil, "", err
	}
	return snapshot, cipher, identity.Recipient().String(), nil
}

func decodePassphrase(path, passphrase string) (Snapshot, []byte, error) {
	var snapshot Snapshot
	if passphrase == "" {
		return snapshot, nil, errors.New("empty passphrase")
	}
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return snapshot, nil, err
	}
	return decodeWithIdentity(path, identity)
}

const (
	ArchiveEncryptionPassphrase = "passphrase"
	ArchiveEncryptionRecipient  = "recipient"
)

type archiveProbeIdentity struct{}

func (archiveProbeIdentity) Unwrap([]*age.Stanza) ([]byte, error) {
	return nil, age.ErrIncorrectIdentity
}

func ArchiveEncryptionMode(path string) (string, error) {
	file, err := platformfs.OpenExternalPrivate(path, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || platformfs.CheckFile(file.File) != nil || info.Size() > limit+1<<20 {
		return "", errors.New("invalid private archive")
	}
	// age.Decrypt parses the header and asks the probe identity to unwrap the
	// file key. The probe always rejects, so payload bytes are never consumed.
	_, err = age.Decrypt(file, archiveProbeIdentity{})
	var noMatch *age.NoIdentityMatchError
	if !errors.As(err, &noMatch) || len(noMatch.StanzaTypes) == 0 {
		return "", errors.New("invalid age archive")
	}
	all := func(expected string) bool {
		for _, stanzaType := range noMatch.StanzaTypes {
			if stanzaType != expected {
				return false
			}
		}
		return true
	}
	switch {
	case all("scrypt"):
		return ArchiveEncryptionPassphrase, nil
	case all("X25519"):
		return ArchiveEncryptionRecipient, nil
	default:
		return "", errors.New("unsupported age recipient")
	}
}

func Inspect(path, identityPath string) (Metadata, *protocol.Error) {
	snapshot, _, _, err := decode(path, identityPath)
	if err != nil {
		return Metadata{}, failure("invalid_backup")
	}
	defer destroySnapshot(&snapshot)
	return Metadata{CreatedAt: snapshot.Created, Profiles: summaries(snapshot)}, nil
}

func selectImportSummary(snapshot Snapshot, requested string) (Summary, *protocol.Error) {
	if requested == "" {
		if len(snapshot.Profiles) != 1 {
			return Summary{}, protocol.NewError("invalid_argument", "Specify --profile when the import file contains multiple profiles.", 2, map[string]any{"field": "profile"})
		}
		profile := snapshot.Profiles[0]
		return Summary{Profile: profile.Name, Values: profile.Values != nil, Tasks: profile.Tasks != nil}, nil
	}
	for _, profile := range snapshot.Profiles {
		if profile.Name == requested {
			return Summary{Profile: profile.Name, Values: profile.Values != nil, Tasks: profile.Tasks != nil}, nil
		}
	}
	return Summary{}, protocol.NewError("profile_not_found", "The selected profile is not present in the import file.", 3, map[string]any{"profile": requested})
}

func (e Engine) Import(ctx context.Context, request ImportRequest) (ImportResult, *protocol.Error) {
	result := ImportResult{Plan: Plan{Targets: []Target{}}, Diff: profilecatalog.Diff{Envs: []profilecatalog.NameChange{}, Variables: []profilecatalog.ValueChange{}, Secrets: []profilecatalog.ValueChange{}, Items: []profilecatalog.TaskChange{}, Instances: []profilecatalog.InstanceChange{}}}
	if !filepath.IsAbs(e.Data) || !filepath.IsAbs(e.Cache) || (request.Expected == "") != (request.RequestID == "") || (request.IdentityPath == "") == (request.Passphrase == "") {
		return result, failure("invalid_argument")
	}
	if request.RequestID != "" && !restoreRequestIDPattern.MatchString(request.RequestID) {
		return result, failure("invalid_argument")
	}
	if request.Expected != "" {
		digestBytes, digestErr := hex.DecodeString(request.Expected)
		if digestErr != nil || len(digestBytes) != sha256.Size {
			return result, failure("invalid_argument")
		}
	}
	if request.Source != "" && !project.ValidProfile(request.Source) || request.Target != "" && !project.ValidProfile(request.Target) {
		return result, failure("invalid_argument")
	}

	var (
		snapshot          Snapshot
		cipher            []byte
		identityRecipient string
		err               error
	)
	if request.Passphrase != "" {
		snapshot, cipher, err = decodePassphrase(request.Path, request.Passphrase)
	} else {
		snapshot, cipher, identityRecipient, err = decode(request.Path, request.IdentityPath)
	}
	if err != nil {
		return result, failure("invalid_backup")
	}
	defer destroySnapshot(&snapshot)
	source, selectionErr := selectImportSummary(snapshot, request.Source)
	if selectionErr != nil {
		return result, selectionErr
	}
	target := request.Target
	if target == "" {
		target = source.Profile
	}
	var selected *Profile
	for index := range snapshot.Profiles {
		if snapshot.Profiles[index].Name == source.Profile {
			selected = &snapshot.Profiles[index]
			break
		}
	}
	if selected == nil {
		return result, failure("invalid_backup")
	}
	sourceCanonical, canonicalErr := profilecatalog.CanonicalSnapshot(source.Profile, selected.Values, selected.Tasks)
	if canonicalErr != nil {
		return result, failure("invalid_backup")
	}
	operationFingerprint := restoreFingerprint(restoreFingerprintInput{Cipher: cipher, Source: source.Profile, Target: target, Expected: request.Expected, Replace: request.Replace})
	var recovery *recoveryOptions
	if request.RecoveryDirectory != "" {
		if !filepath.IsAbs(request.RecoveryDirectory) {
			return result, failure("invalid_argument")
		}
		recovery = &recoveryOptions{Directory: request.RecoveryDirectory, Recipient: identityRecipient, Passphrase: request.Passphrase, Prefix: "before-import-"}
	}
	plan, restoreErr := e.restore(ctx, restoreRequest{Snapshot: snapshot, Cipher: cipher, Source: source.Profile, Target: target, Expected: request.Expected, RequestID: request.RequestID, OperationFingerprint: operationFingerprint, Replace: request.Replace, Import: &importContext{Source: source, Canonical: sourceCanonical}, Recovery: recovery})
	if restoreErr != nil {
		return result, restoreErr
	}
	if plan.importData == nil {
		return result, failure("storage_error")
	}
	return ImportResult{Plan: plan, Source: plan.importData.Source, Target: plan.importData.Target, Diff: plan.importData.Diff}, nil
}

func (e Engine) Restore(ctx context.Context, path, identityPath, source, target, expected, requestID string, replace bool) (Plan, *protocol.Error) {
	plan := Plan{Targets: []Target{}}
	if !filepath.IsAbs(e.Data) || !filepath.IsAbs(e.Cache) {
		return plan, failure("invalid_argument")
	}
	snapshot, cipher, _, err := decode(path, identityPath)
	if err != nil {
		return plan, failure("invalid_backup")
	}
	defer destroySnapshot(&snapshot)
	return e.restore(ctx, restoreRequest{Snapshot: snapshot, Cipher: cipher, Source: source, Target: target, Expected: expected, RequestID: requestID, Replace: replace})
}

func (e Engine) restore(ctx context.Context, request restoreRequest) (Plan, *protocol.Error) {
	plan := Plan{Targets: []Target{}}
	s, cipher := request.Snapshot, request.Cipher
	source, target := request.Source, request.Target
	expected, requestID, replace := request.Expected, request.RequestID, request.Replace
	if source == "" || !project.ValidProfile(source) {
		return plan, failure("invalid_argument")
	}
	if target == "" {
		target = source + "-restored"
	}
	if !project.ValidProfile(target) {
		return plan, failure("invalid_argument")
	}
	var selected *Profile
	for i := range s.Profiles {
		if s.Profiles[i].Name == source {
			selected = &s.Profiles[i]
		}
	}
	if selected == nil {
		return plan, failure("not_found")
	}
	if values.IsLogicalSnapshot(selected.Values) && e.VaultMaster == nil {
		return plan, failure("unsupported_storage")
	}
	scoped, closeScope, scopeErr := e.withVault(ctx)
	if scopeErr != nil {
		return plan, scopeErr
	}
	defer closeScope()
	e = scoped
	if e.vaultMaster != nil && expected != "" {
		releaseLaunch, err := vault.AcquireLaunchMutation(ctx, filepath.Join(e.Data, "vault"))
		if err != nil {
			return plan, values.VaultError(ctx, err)
		}
		defer releaseLaunch()
	}
	release, err := maintenance.Acquire(ctx, e.Data)
	if err != nil {
		return plan, failure("storage_error")
	}
	defer release()
	// The legacy restore writer must not recreate plaintext values, including
	// a previously absent target profile, inside a protected root. The keyed
	// logical restore adapter will own this path once configured.
	if err := e.checkVaultRootHeld(); err != nil {
		if err.Error() == "unsupported_storage" {
			return plan, failure("unsupported_storage")
		}
		return plan, values.VaultError(ctx, err)
	}
	targetTasks := tasks.Store{Directory: filepath.Join(e.Data, "tasks"), Profile: target}
	if err := targetTasks.CollectOrphanGenerationsHeld(); err != nil {
		return plan, failure("storage_error")
	}
	fingerprint := restoreFingerprint(restoreFingerprintInput{Cipher: cipher, Source: source, Target: target, Expected: expected, Replace: replace})
	receiptPath := "backup-receipts/" + requestID + ".json"
	if expected != "" {
		if !restoreRequestIDPattern.MatchString(requestID) {
			return plan, failure("invalid_argument")
		}
		b, err := maintenance.Read(filepath.Join(e.Data, receiptPath), restoreReceiptLimit)
		if err == nil {
			decoded, decodeErr := e.decodeReceipt(ctx, requestID, b)
			if decodeErr != nil {
				return plan, failure("storage_error")
			}
			defer clear(decoded)
			b = decoded
			var previous restoreReceipt
			if json.Unmarshal(b, &previous) != nil {
				return plan, failure("storage_error")
			}
			if request.OperationFingerprint != "" {
				if previous.OperationFingerprint != request.OperationFingerprint {
					return plan, failure("request_conflict")
				}
			} else if previous.OperationFingerprint != "" || previous.Fingerprint != fingerprint {
				return plan, failure("request_conflict")
			}
			previous.Plan.Replayed = true
			previous.Plan.importData = previous.Import
			return previous.Plan, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return plan, failure("storage_error")
		}
	}
	exists := false
	targetData := map[string][]byte{}
	defer func() {
		for _, body := range targetData {
			clear(body)
		}
	}()
	hashInput := []byte(digest(cipher) + "\n" + source + "\n" + target)
	valuesData, snapshotErr := e.valueSnapshotHeld(ctx, target)
	if snapshotErr != nil {
		return plan, failure("storage_error")
	}
	defer clear(valuesData)
	if valuesData != nil {
		exists = true
		targetData["profiles"] = append([]byte{}, valuesData...)
	}
	hashInput = append(hashInput, []byte("\nprofiles:"+digest(valuesData))...)
	taskSnapshot, snapshotErr := targetTasks.ExportSnapshotHeld(limit)
	if snapshotErr != nil {
		return plan, failure("storage_error")
	}
	if taskSnapshot.Data != nil {
		exists = true
		targetData["tasks"] = append([]byte{}, taskSnapshot.Data...)
	}
	hashInput = append(hashInput, []byte("\ntasks:"+digest(taskSnapshot.Data))...)
	if e.vaultMaster != nil {
		identity, identityErr := e.vaultStore().SnapshotIdentityHeld(ctx, target)
		if identityErr != nil {
			return plan, values.VaultError(ctx, identityErr)
		}
		hashInput = append(hashInput, []byte("\nidentity:"+identity.ProfileID+":"+identity.StorageGeneration)...)
		plan.Digest, err = vault.BackupFingerprint(e.vaultMaster, e.vaultID, target, hashInput)
		if err != nil {
			return plan, values.VaultError(ctx, err)
		}
	} else {
		keyPath := filepath.Join(e.Data, ".maintenance", "preview-key")
		key, keyErr := maintenance.Read(keyPath, 32)
		if errors.Is(keyErr, os.ErrNotExist) {
			key = make([]byte, 32)
			if _, keyErr = rand.Read(key); keyErr == nil {
				keyErr = maintenance.Write(keyPath, key)
			}
		}
		if keyErr != nil || len(key) != 32 {
			return plan, failure("storage_error")
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(hashInput)
		plan.Digest = hex.EncodeToString(mac.Sum(nil))
	}
	plan.Targets = append(plan.Targets, Target{source, target, exists})
	if request.Import != nil {
		targetCanonical, canonicalErr := profilecatalog.CanonicalSnapshot(target, targetData["profiles"], targetData["tasks"])
		if canonicalErr != nil {
			return plan, failure("storage_error")
		}
		plan.importData = &importReceiptData{Source: request.Import.Source, Target: target, Diff: profilecatalog.Compare(targetCanonical, request.Import.Canonical, false)}
	}
	if expected == "" {
		return plan, nil
	}
	if expected != plan.Digest {
		return plan, failure("revision_conflict")
	}
	if exists && !replace {
		return plan, failure("profile_exists")
	}
	if exists {
		directory, recoveryRecipient, recoveryPassphrase, prefix := "", "", "", "before-restore-"
		if request.Recovery != nil {
			directory = request.Recovery.Directory
			recoveryRecipient = request.Recovery.Recipient
			recoveryPassphrase = request.Recovery.Passphrase
			if request.Recovery.Prefix != "" {
				prefix = request.Recovery.Prefix
			}
			if err := ensurePrivateDirectoryChain(e.Data, directory); err != nil {
				return plan, failure("backup_error")
			}
			if (recoveryRecipient == "") == (recoveryPassphrase == "") {
				return plan, failure("backup_error")
			}
			if recoveryRecipient != "" {
				canonical, err := canonicalRecipient(recoveryRecipient)
				if err != nil || canonical != recoveryRecipient {
					return plan, failure("backup_error")
				}
			}
		} else {
			c, err := e.config()
			if err != nil {
				return plan, failure("backup_not_configured")
			}
			directory, recoveryRecipient = c.Directory, c.Recipient
			if platformfs.EnsureExternalDirectory(directory) != nil {
				return plan, failure("backup_error")
			}
		}
		before, err := e.snapshot(target)
		if err != nil {
			return plan, failure("backup_error")
		}
		defer destroySnapshot(&before)
		var b []byte
		if recoveryPassphrase != "" {
			b, err = encodePassphrase(before, recoveryPassphrase)
		} else {
			b, err = encode(before, recoveryRecipient)
		}
		if err != nil {
			return plan, failure("backup_error")
		}
		plan.SafetyBackup = filepath.Join(directory, prefix+tasks.ID()+".age")
		if err := exclusiveContext(ctx, plan.SafetyBackup, b); err != nil {
			if ctx.Err() != nil {
				return plan, failure("canceled")
			}
			return plan, failure("backup_error")
		}
	}
	appliedPlan := plan
	appliedPlan.Applied = true
	receipt, err := json.Marshal(restoreReceipt{Fingerprint: fingerprint, OperationFingerprint: request.OperationFingerprint, Plan: appliedPlan, Import: plan.importData})
	if err != nil {
		return plan, failure("storage_error")
	}
	if int64(len(receipt)) > restoreReceiptLimit {
		return plan, protocol.NewError("result_too_large", "Restore result exceeds the replay receipt size limit.", 3, map[string]any{"limit_bytes": restoreReceiptLimit})
	}
	protectedReceipt, receiptErr := e.encodeReceipt(ctx, requestID, receipt)
	if receiptErr != nil {
		return plan, values.VaultError(ctx, receiptErr)
	}
	if e.vaultMaster != nil {
		clear(receipt)
	}
	files := map[string][]byte{receiptPath: protectedReceipt}
	var valuesPayload []byte
	defer func() { clear(valuesPayload) }()
	if selected.Values != nil {
		valuesPayload, err = values.RestoreSnapshot(selected.Values, source, target)
		if err != nil {
			return plan, failure("invalid_backup")
		}
	}
	valueFiles, replacementErr := e.restoreValueFilesHeld(ctx, target, valuesPayload)
	if replacementErr != nil {
		return plan, failure("storage_error")
	}
	for path, body := range valueFiles {
		files[path] = body
	}

	if selected.Tasks != nil {
		taskPayload, restoreErr := tasks.RestoreSnapshot(selected.Tasks, source, target)
		if restoreErr != nil {
			return plan, failure("invalid_backup")
		}
		staged, stageErr := targetTasks.StageExactSnapshotHeld(taskPayload)
		if stageErr != nil {
			return plan, failure("storage_error")
		}
		for path, body := range staged.Files {
			files[path] = body
		}
	} else {
		taskFiles, deleteErr := targetTasks.DeletionFilesHeld()
		if deleteErr != nil {
			return plan, failure("storage_error")
		}
		for path, body := range taskFiles {
			files[path] = body
		}
	}
	if ctx.Err() != nil {
		return plan, failure("canceled")
	}
	// Query snapshots refer to pre-restore revisions and are regenerated.
	for _, p := range []string{filepath.Join(e.Cache, "task-queries"), filepath.Join(e.Cache, "dashboard", "queries")} {
		if os.RemoveAll(p) != nil {
			return plan, failure("storage_error")
		}
	}
	plan.Applied = true
	if maintenance.Replace(e.Data, files) != nil {
		plan.Applied = false
		return plan, failure("storage_error")
	}
	// The restore and its receipt are committed together above. Generation
	// reclamation is post-commit housekeeping; a later exclusive task/backup
	// entry retries it before accepting a new mutation.
	_ = targetTasks.CollectOrphanGenerationsHeld()
	return plan, nil
}
