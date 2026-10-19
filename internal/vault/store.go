package vault

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
)

type CatalogEntry struct {
	Name       string `json:"profile"`
	ID         string `json:"profile_id"`
	Generation string `json:"storage_generation"`
	Revision   uint64 `json:"revision"`
	KeyEpoch   uint64 `json:"key_epoch"`
	Sealed     bool   `json:"sealed"`
	Tag        []byte `json:"manifest_tag,omitempty"`
}
type Catalog struct {
	Format  int            `json:"vault_catalog_format"`
	VaultID string         `json:"vault_id"`
	Entries []CatalogEntry `json:"profiles"`
	Tag     []byte         `json:"tag"`
}

func (c *Catalog) canonical() ([]byte, error) {
	if c.Format != StorageVersion || !validID(c.VaultID) {
		return nil, ErrInvalid
	}
	e := NewEncoder("vault-catalog")
	e.Uint(uint64(c.Format))
	e.String(c.VaultID)
	entries := append([]CatalogEntry{}, c.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	e.Uint(uint64(len(entries)))
	names, ids := map[string]bool{}, map[string]bool{}
	for _, p := range entries {
		if !namePattern.MatchString(p.Name) || names[p.Name] || !validID(p.ID) || ids[p.ID] || !validID(p.Generation) || p.Revision == 0 {
			return nil, ErrInvalid
		}
		if p.Sealed && (p.KeyEpoch == 0 || len(p.Tag) != sha256.Size) || !p.Sealed && (p.KeyEpoch != 0 || len(p.Tag) != 0) {
			return nil, ErrInvalid
		}
		names[p.Name] = true
		ids[p.ID] = true
		e.String(p.Name)
		e.String(p.ID)
		e.String(p.Generation)
		e.Uint(p.Revision)
		e.Uint(p.KeyEpoch)
		e.Bool(p.Sealed)
		e.Blob(p.Tag)
	}
	return e.Finish()
}
func (c *Catalog) mac(key *Material) ([]byte, error) {
	data, err := c.canonical()
	if err != nil {
		return nil, err
	}
	k, err := derive(key, c.VaultID, "catalog", "catalog")
	if err != nil {
		return nil, err
	}
	defer clear(k)
	h := hmac.New(sha256.New, k)
	h.Write(data)
	return h.Sum(nil), nil
}
func (c *Catalog) Sign(key *Material) error {
	b, err := c.mac(key)
	if err == nil {
		c.Tag = b
	}
	return err
}
func (c *Catalog) Verify(key *Material) error {
	b, err := c.mac(key)
	if err != nil {
		return err
	}
	if !hmac.Equal(c.Tag, b) {
		return ErrIntegrity
	}
	return nil
}

// FileStore publishes an authenticated catalog and encrypted profiles together.
// Root is the existing data root. Initialize is explicit; missing catalog/profile
// files during ordinary reads are never treated as an empty vault/profile.
// The caller owns protector/session access and must supply the verified keyset.
type FileStore struct {
	Root       string
	VaultID    string
	CatalogKey *Material
}

const catalogRelative = "profiles/vault-catalog.json"

func (s FileStore) readCatalog() (*Catalog, error) {
	b, err := maintenance.Read(filepath.Join(s.Root, catalogRelative), MaxDocumentBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrKeyMissing
	}
	if err != nil {
		return nil, ErrRecovery
	}
	var c Catalog
	if err = StrictDecode(b, &c); err != nil {
		return nil, err
	}
	if c.VaultID != s.VaultID {
		return nil, ErrIntegrity
	}
	if err = c.Verify(s.CatalogKey); err != nil {
		return nil, err
	}
	return &c, nil
}
func (s FileStore) Initialize(ctx context.Context) error {
	if !validID(s.VaultID) {
		return ErrInvalid
	}
	releaseLaunch, err := AcquireLaunchMutation(ctx, filepath.Join(s.Root, "vault"))
	if err != nil {
		return err
	}
	defer releaseLaunch()
	release, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		return err
	}
	defer release()
	if _, err = os.Lstat(filepath.Join(s.Root, catalogRelative)); err == nil {
		return errorsNamed("vault_already_initialized")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrRecovery
	}
	if _, protected, err := maintenance.ProtectedRootHeld(s.Root); err != nil {
		return err
	} else if protected {
		return ErrKeyMissing
	}
	names, err := profilekey.Enumerate(filepath.Join(s.Root, "profiles"), "values")
	if err != nil {
		return ErrRecovery
	}
	if len(names) > 0 {
		return errorsNamed("migration_required")
	}
	c := Catalog{Format: StorageVersion, VaultID: s.VaultID, Entries: []CatalogEntry{}}
	if err = c.Sign(s.CatalogKey); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	id, err := NewID()
	if err != nil {
		return err
	}
	_, err = (ForwardStore{Root: s.Root, VaultID: s.VaultID, Master: s.CatalogKey}).ReplaceHeld(ctx, id, hex.EncodeToString(c.Tag), map[string][]byte{catalogRelative: b})
	return err
}
func entryFor(p *Profile) CatalogEntry {
	return CatalogEntry{Name: p.Name, ID: p.ID, Generation: p.Generation, Revision: p.Revision, KeyEpoch: p.KeyEpoch, Sealed: p.Sealed, Tag: append([]byte(nil), p.Tag...)}
}
func (s FileStore) readProfileEnvelope(c *Catalog, name string) (*Profile, int, error) {
	index := -1
	for i, e := range c.Entries {
		if e.Name == name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, -1, errorsNamed("profile_not_found")
	}
	b, err := maintenance.Read(filepath.Join(s.Root, "profiles", profilekey.CanonicalFilename(name)), MaxDocumentBytes)
	if err != nil {
		return nil, index, ErrRecovery
	}
	p, err := DecodeProfile(b)
	if err != nil {
		return nil, index, err
	}
	want, got := c.Entries[index], entryFor(p)
	if want.Name != got.Name || want.ID != got.ID || want.Generation != got.Generation || want.Revision != got.Revision || want.KeyEpoch != got.KeyEpoch || want.Sealed != got.Sealed || !hmac.Equal(want.Tag, got.Tag) || (p.Sealed && p.VaultID != s.VaultID) {
		return nil, index, ErrIntegrity
	}
	return p, index, nil
}
func (s FileStore) readProfile(c *Catalog, name string, key *Material) (*Profile, int, error) {
	p, index, err := s.readProfileEnvelope(c, name)
	if err != nil {
		return nil, index, err
	}
	if err = p.Verify(key); err != nil {
		return nil, index, err
	}
	return p, index, nil
}

// ReadWithMaster authenticates catalog, profile identity and keyset under one
// shared disk gate. The caller obtains the root key before entering this gate;
// no OS authentication or session IPC occurs inside it.
func (s FileStore) ReadWithMaster(ctx context.Context, name string, master *Material) (*Profile, *Material, error) {
	if !namePattern.MatchString(name) {
		return nil, nil, ErrInvalid
	}
	release, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return nil, nil, err
	}
	defer release()
	return s.ReadWithMasterHeld(ctx, name, master)
}

// ReadWithMasterHeld requires a shared/exclusive data-root maintenance gate.
// The root key must have been acquired before that gate.
func (s FileStore) ReadWithMasterHeld(ctx context.Context, name string, master *Material) (*Profile, *Material, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !namePattern.MatchString(name) {
		return nil, nil, ErrInvalid
	}
	c, err := s.readCatalog()
	if err != nil {
		return nil, nil, err
	}
	p, _, err := s.readProfileEnvelope(c, name)
	if err != nil {
		return nil, nil, err
	}
	var key *Material
	if p.Sealed {
		key, err = UnwrapKeyset(master, p.VaultID, p.ID, p.KeyEpoch, p.WrappedKey)
		if err != nil {
			return nil, nil, err
		}
	}
	if err = p.Verify(key); err != nil {
		key.Destroy()
		return nil, nil, err
	}
	return p, key, nil
}
func (s FileStore) Read(ctx context.Context, name string, key *Material) (*Profile, error) {
	if !namePattern.MatchString(name) {
		return nil, ErrInvalid
	}
	release, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return nil, err
	}
	defer release()
	c, err := s.readCatalog()
	if err != nil {
		return nil, err
	}
	p, _, err := s.readProfile(c, name, key)
	return p, err
}
func (s FileStore) publish(ctx context.Context, c *Catalog, p *Profile, key *Material) error {
	return s.publishWithFiles(ctx, c, p, key, nil)
}
func (s FileStore) publishWithFiles(ctx context.Context, c *Catalog, p *Profile, key *Material, extra map[string][]byte) error {
	if p.Sealed && p.VaultID != s.VaultID {
		return ErrIntegrity
	}
	if err := p.Verify(key); err != nil {
		return err
	}
	if err := c.Sign(s.CatalogKey); err != nil {
		return err
	}
	pb, err := p.Encode()
	if err != nil {
		return err
	}
	cb, err := json.Marshal(c)
	if err != nil {
		return ErrInvalid
	}
	if len(cb) > MaxDocumentBytes {
		return ErrTooLarge
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Ordinary writes only accept already encrypted profiles. Legacy plaintext
	// migration must use its own encrypted recovery journal, not this helper.
	files, err := profilekey.Replacement(filepath.Join(s.Root, "profiles"), p.Name, "values", pb)
	if err != nil {
		return ErrRecovery
	}
	files[catalogRelative] = cb
	for path, data := range extra {
		if _, exists := files[path]; exists {
			return ErrInvalid
		}
		files[path] = data
	}
	return maintenance.Replace(s.Root, files)
}
func (s FileStore) Create(ctx context.Context, p *Profile, key *Material) error {
	if err := p.Verify(key); err != nil {
		return err
	}
	release, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		return err
	}
	defer release()
	c, err := s.readCatalog()
	if err != nil {
		return err
	}
	for _, e := range c.Entries {
		if e.Name == p.Name || e.ID == p.ID {
			return errorsNamed("profile_exists")
		}
	}
	if _, err = os.Lstat(filepath.Join(s.Root, "profiles", profilekey.CanonicalFilename(p.Name))); err == nil {
		return errorsNamed("profile_exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrRecovery
	}
	resolved, resolveErr := profilekey.Resolve(filepath.Join(s.Root, "profiles"), p.Name, "values")
	if resolveErr != nil {
		return ErrRecovery
	}
	if resolved.Mode != profilekey.ModeNone {
		return errorsNamed("migration_required")
	}
	c.Entries = append(c.Entries, entryFor(p))
	return s.publish(ctx, c, p, key)
}

// Update is a single profile commit. The callback must not perform IPC/process
// work; it runs under the existing exclusive maintenance gate.
func (s FileStore) Update(ctx context.Context, name string, key *Material, change func(*Profile) (bool, error)) (bool, error) {
	if !namePattern.MatchString(name) {
		return false, ErrInvalid
	}
	release, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		return false, err
	}
	defer release()
	c, err := s.readCatalog()
	if err != nil {
		return false, err
	}
	p, index, err := s.readProfile(c, name, key)
	if err != nil {
		return false, err
	}
	original := entryFor(p)
	changed, err := change(p)
	if err != nil || !changed {
		return false, err
	}
	if p.Name != original.Name || p.ID != original.ID || p.Generation != original.Generation || p.KeyEpoch != original.KeyEpoch || p.Sealed != original.Sealed || p.Revision <= original.Revision {
		return false, ErrInvalid
	}
	if err = p.Sign(key); err != nil {
		return false, err
	}
	c.Entries[index] = entryFor(p)
	if err = s.publish(ctx, c, p, key); err != nil {
		return false, err
	}
	return true, nil
}
