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
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

const limit int64 = 128 << 20

type Engine struct{ Data, Config, Cache string }
type Config struct {
	Directory string `json:"directory"`
	Recipient string `json:"recipient"`
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
type Plan struct {
	Digest       string   `json:"digest"`
	Targets      []Target `json:"targets"`
	Applied      bool     `json:"applied"`
	SafetyBackup string   `json:"safety_backup,omitempty"`
}
type Target struct {
	Source  string `json:"source"`
	Profile string `json:"profile"`
	Exists  bool   `json:"exists"`
}

func failure(code string) *protocol.Error {
	return protocol.NewError(code, "Backup operation could not be completed; check the selected files and target state.", 3, nil)
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func file(domain, profile string) string {
	return domain + "/" + hex.EncodeToString([]byte(profile)) + ".json"
}

func exclusive(path string, b []byte) error {
	// Publish a complete file through a hard link, refusing any existing target.
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".backup-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	c := f.Close()
	if e != nil {
		return e
	}
	if c != nil {
		return c
	}
	if e = os.Link(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
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
func recipient(path string) (string, error) {
	b, e := maintenance.Read(path, 4096)
	if e != nil {
		return "", e
	}
	r, e := age.ParseX25519Recipient(strings.TrimSpace(string(b)))
	if e != nil {
		return "", e
	}
	return r.String(), nil
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
	if err = os.MkdirAll(e.Config, 0700); err != nil {
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
	return c, err
}

func (e Engine) snapshot(selected string) (Snapshot, error) {
	s := Snapshot{Version: 1, Created: time.Now().UTC().Format(time.RFC3339Nano), Profiles: []Profile{}}
	names := map[string]bool{}
	if selected != "" {
		names[selected] = true
	} else {
		for _, domain := range []string{"profiles", "tasks"} {
			if info, err := os.Lstat(filepath.Join(e.Data, domain)); err == nil {
				if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
					return s, errors.New("private domain required")
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return s, err
			}
			entries, err := os.ReadDir(filepath.Join(e.Data, domain))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return s, err
			}
			for _, entry := range entries {
				if !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				b, err := hex.DecodeString(strings.TrimSuffix(entry.Name(), ".json"))
				if err != nil || !project.ValidProfile(string(b)) {
					return s, errors.New("invalid profile filename")
				}
				names[string(b)] = true
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
		for _, domain := range []string{"profiles", "tasks"} {
			if info, err := os.Lstat(filepath.Join(e.Data, domain)); err == nil {
				if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
					return s, errors.New("private domain required")
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return s, err
			}
			b, err := maintenance.Read(filepath.Join(e.Data, file(domain, name)), limit)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return s, err
			}
			if domain == "profiles" {
				_, err = values.RestoreSnapshot(b, name, name)
				p.Values = b
			} else {
				_, err = tasks.RestoreSnapshot(b, name, name)
				p.Tasks = b
			}
			if err != nil {
				return s, err
			}
		}
		if p.Values == nil && p.Tasks == nil {
			return s, os.ErrNotExist
		}
		s.Profiles = append(s.Profiles, p)
	}
	return s, nil
}
func encode(s Snapshot, r string) ([]byte, error) {
	rec, err := age.ParseX25519Recipient(r)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(s)
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("backup too large")
	}
	var out bytes.Buffer
	w, err := age.Encrypt(&out, rec)
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
func summaries(s Snapshot) []Summary {
	out := []Summary{}
	for _, p := range s.Profiles {
		out = append(out, Summary{p.Name, p.Values != nil, p.Tasks != nil})
	}
	return out
}
func (e Engine) Create(ctx context.Context, profile, output, recipientPath string) (any, *protocol.Error) {
	if profile != "" && !project.ValidProfile(profile) {
		return nil, failure("invalid_argument")
	}
	c, configErr := e.config()
	if recipientPath != "" {
		r, err := recipient(recipientPath)
		if err != nil {
			return nil, failure("invalid_recipient")
		}
		c.Recipient = r
	} else if configErr != nil {
		return nil, failure("backup_not_configured")
	}
	if output == "" {
		if configErr != nil {
			return nil, failure("backup_not_configured")
		}
		if os.MkdirAll(c.Directory, 0700) != nil {
			return nil, failure("backup_error")
		}
		output = filepath.Join(c.Directory, "devtools-"+tasks.ID()+".age")
	}
	release, err := maintenance.Acquire(ctx, e.Data)
	if err != nil {
		return nil, failure("storage_error")
	}
	defer release()
	s, err := e.snapshot(profile)
	if err != nil {
		return nil, failure("backup_error")
	}
	b, err := encode(s, c.Recipient)
	if err != nil {
		return nil, failure("backup_error")
	}
	if exclusive(output, b) != nil {
		return nil, failure("backup_error")
	}
	return map[string]any{"path": output, "profiles": summaries(s), "created_at": s.Created}, nil
}
func decode(path, identityPath string) (Snapshot, []byte, error) {
	var s Snapshot
	b, e := maintenance.Read(path, limit+1<<20)
	if e != nil {
		return s, nil, e
	}
	key, e := maintenance.Read(identityPath, 4096)
	if e != nil {
		return s, nil, e
	}
	id, e := age.ParseX25519Identity(strings.TrimSpace(string(key)))
	if e != nil {
		return s, nil, e
	}
	reader, e := age.Decrypt(bytes.NewReader(b), id)
	if e != nil {
		return s, nil, e
	}
	// Read to authenticated EOF before any parsing or mutation.
	plain, e := io.ReadAll(io.LimitReader(reader, limit+1))
	if e != nil || int64(len(plain)) > limit {
		return s, nil, errors.New("invalid encrypted backup")
	}
	d := json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF || s.Version != 1 {
		return s, nil, errors.New("invalid backup")
	}
	seen := map[string]bool{}
	for _, p := range s.Profiles {
		if !project.ValidProfile(p.Name) || seen[p.Name] || p.Values == nil && p.Tasks == nil {
			return s, nil, errors.New("invalid profile")
		}
		seen[p.Name] = true
		if p.Values != nil {
			if _, e = values.RestoreSnapshot(p.Values, p.Name, p.Name); e != nil {
				return s, nil, e
			}
		}
		if p.Tasks != nil {
			if _, e = tasks.RestoreSnapshot(p.Tasks, p.Name, p.Name); e != nil {
				return s, nil, e
			}
		}
	}
	return s, b, nil
}
func Inspect(path, identityPath string) (any, *protocol.Error) {
	s, _, e := decode(path, identityPath)
	if e != nil {
		return nil, failure("invalid_backup")
	}
	return map[string]any{"created_at": s.Created, "profiles": summaries(s)}, nil
}

func (e Engine) Restore(ctx context.Context, path, identityPath, source, target, expected, requestID string, replace bool) (Plan, *protocol.Error) {
	plan := Plan{Targets: []Target{}}
	if !filepath.IsAbs(e.Data) || !filepath.IsAbs(e.Cache) {
		return plan, failure("invalid_argument")
	}
	s, cipher, err := decode(path, identityPath)
	if err != nil {
		return plan, failure("invalid_backup")
	}
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
	release, err := maintenance.Acquire(ctx, e.Data)
	if err != nil {
		return plan, failure("storage_error")
	}
	defer release()
	type receipt struct {
		Fingerprint string `json:"fingerprint"`
		Plan        Plan   `json:"plan"`
	}
	fingerprint := digest([]byte(digest(cipher) + "\n" + source + "\n" + target + "\n" + expected + "\n" + map[bool]string{true: "replace", false: "create"}[replace]))
	receiptPath := "backup-receipts/" + requestID + ".json"
	if expected != "" {
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(requestID) {
			return plan, failure("invalid_argument")
		}
		b, err := maintenance.Read(filepath.Join(e.Data, receiptPath), 1<<20)
		if err == nil {
			var previous receipt
			if json.Unmarshal(b, &previous) != nil {
				return plan, failure("storage_error")
			}
			if previous.Fingerprint != fingerprint {
				return plan, failure("request_conflict")
			}
			return previous.Plan, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return plan, failure("storage_error")
		}
	}
	exists := false
	hashInput := []byte(digest(cipher) + "\n" + source + "\n" + target)
	for _, domain := range []string{"profiles", "tasks"} {
		b, err := maintenance.Read(filepath.Join(e.Data, file(domain, target)), limit)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return plan, failure("storage_error")
		}
		exists = exists || err == nil
		hashInput = append(hashInput, []byte("\n"+domain+":"+digest(b))...)
	}
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
	mac.Write(append(hashInput, []byte("\n"+map[bool]string{true: "replace", false: "create"}[replace])...))
	plan.Digest = hex.EncodeToString(mac.Sum(nil))
	plan.Targets = append(plan.Targets, Target{source, target, exists})
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
		c, err := e.config()
		if err != nil {
			return plan, failure("backup_not_configured")
		}
		before, err := e.snapshot(target)
		if err != nil {
			return plan, failure("backup_error")
		}
		b, err := encode(before, c.Recipient)
		if err != nil {
			return plan, failure("backup_error")
		}
		if os.MkdirAll(c.Directory, 0700) != nil {
			return plan, failure("backup_error")
		}
		plan.SafetyBackup = filepath.Join(c.Directory, "before-restore-"+tasks.ID()+".age")
		if exclusive(plan.SafetyBackup, b) != nil {
			return plan, failure("backup_error")
		}
	}
	files := map[string][]byte{file("profiles", target): nil, file("tasks", target): nil}
	if selected.Values != nil {
		files[file("profiles", target)], err = values.RestoreSnapshot(selected.Values, source, target)
		if err != nil {
			return plan, failure("invalid_backup")
		}
	}
	if selected.Tasks != nil {
		files[file("tasks", target)], err = tasks.RestoreSnapshot(selected.Tasks, source, target)
		if err != nil {
			return plan, failure("invalid_backup")
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
	files[receiptPath], err = json.Marshal(receipt{fingerprint, plan})
	if err != nil {
		return plan, failure("storage_error")
	}
	if maintenance.Replace(e.Data, files) != nil {
		plan.Applied = false
		return plan, failure("storage_error")
	}
	return plan, nil
}
