package windowsinstall

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/process"
	"golang.org/x/sys/windows"
)

const manifestName = ".devtools-install.json"
const journalName = ".devtools-install-journal.json"

var transactionID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Options struct {
	Action        string `json:"action"`
	BinDir        string `json:"bin_dir"`
	Version       string `json:"version"`
	Source        string `json:"source,omitempty"`
	ExternalAlias bool   `json:"external_alias,omitempty"`
}
type AliasResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type Result struct {
	Action  string      `json:"action"`
	Version string      `json:"version"`
	Alias   AliasResult `json:"alias"`
	Changed bool        `json:"changed"`
}
type proof struct {
	Volume    uint32 `json:"volume"`
	IndexHigh uint32 `json:"index_high"`
	IndexLow  uint32 `json:"index_low"`
	SHA256    string `json:"sha256"`
}
type retired struct {
	Name  string `json:"name"`
	Proof proof  `json:"proof"`
}
type manifest struct {
	Format   int       `json:"format"`
	Identity identity  `json:"identity"`
	Main     proof     `json:"main"`
	Alias    *proof    `json:"alias,omitempty"`
	Retired  []retired `json:"retired,omitempty"`
}
type journal struct {
	Format   int              `json:"format"`
	ID       string           `json:"id"`
	Phase    string           `json:"phase"`
	Before   *manifest        `json:"before,omitempty"`
	Identity identity         `json:"identity"`
	Alias    bool             `json:"alias"`
	Stages   map[string]proof `json:"stages"`
	After    *manifest        `json:"after,omitempty"`
}
type engine struct {
	root      *platformfs.Root
	directory string
	after     func(string) error
	smoke     func(context.Context, string, identity) error
}

func (e *engine) checkpoint(phase string) error {
	if e.after != nil {
		return e.after(phase)
	}
	return nil
}
func encode(value any) []byte { body, _ := json.Marshal(value); return body }
func decodeStrict(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing installation metadata")
	}
	return nil
}
func (e *engine) readJSON(name string, value any) (bool, error) {
	body, err := e.root.ReadFile(name, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, decodeStrict(body, value)
}
func (e *engine) writeJSON(ctx context.Context, name string, value any) error {
	return e.root.WriteFile(ctx, name, encode(value), true)
}
func fileIdentity(file *os.File) (proof, error) {
	var info windows.ByHandleFileInformation
	err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info)
	return proof{Volume: info.VolumeSerialNumber, IndexHigh: info.FileIndexHigh, IndexLow: info.FileIndexLow}, err
}
func (e *engine) inspect(name string) (proof, bool, error) {
	file, err := e.root.OpenFile(name, os.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return proof{}, false, nil
	}
	if err != nil {
		return proof{}, false, err
	}
	defer file.Close()
	result, err := fileIdentity(file.File)
	if err != nil {
		return proof{}, false, err
	}
	body, err := readBounded(file, maxImage)
	if err != nil {
		return proof{}, false, err
	}
	result.SHA256 = checksum(body)
	return result, true, nil
}
func sameFile(a, b proof) bool {
	return a.Volume == b.Volume && a.IndexHigh == b.IndexHigh && a.IndexLow == b.IndexLow
}
func (e *engine) require(name string, want proof) error {
	got, exists, err := e.inspect(name)
	if err != nil {
		return err
	}
	if !exists || got != want {
		return errors.New("owned executable identity/hash changed: " + name)
	}
	return nil
}
func (e *engine) removeOwned(ctx context.Context, name string, want proof, complete bool) error {
	got, exists, err := e.inspect(name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !sameFile(got, want) || (complete && got != want) {
		return errors.New("recovery artifact identity changed: " + name)
	}
	return e.root.RemoveFile(ctx, name)
}
func validProof(p proof) bool {
	decoded, err := hex.DecodeString(p.SHA256)
	return err == nil && len(decoded) == 32 && (p.IndexHigh != 0 || p.IndexLow != 0)
}
func validManifest(m *manifest) bool {
	if m == nil || m.Format != 1 || !versionToken.MatchString(m.Identity.Version) || !identityToken.MatchString(m.Identity.Commit) || !validProof(m.Main) || (m.Alias != nil && !validProof(*m.Alias)) {
		return false
	}
	if m.Alias != nil && m.Alias.SHA256 != m.Main.SHA256 {
		return false
	}
	seen := map[string]bool{}
	for _, old := range m.Retired {
		if !validRetiredName(old.Name) || !validProof(old.Proof) || seen[old.Name] {
			return false
		}
		seen[old.Name] = true
	}
	return true
}
func validRetiredName(name string) bool {
	for _, target := range []string{"main", "alias"} {
		prefix := ".devtools-"
		suffix := "-" + target + ".old.exe"
		if len(name) == len(prefix)+32+len(suffix) && name[:len(prefix)] == prefix && name[len(name)-len(suffix):] == suffix && transactionID.MatchString(name[len(prefix):len(prefix)+32]) {
			return true
		}
	}
	return false
}
func stageName(id, target string) string { return ".devtools-" + id + "-" + target + ".candidate.exe" }
func oldName(id, target string) string   { return ".devtools-" + id + "-" + target + ".old.exe" }
func executableName(target string) string {
	if target == "main" {
		return "devtools.exe"
	}
	return "dvt.exe"
}
func (j *journal) targets() []string {
	if j.Alias {
		return []string{"main", "alias"}
	}
	return []string{"main"}
}
func (j *journal) oldProof(target string) *proof {
	if j.Before == nil {
		return nil
	}
	if target == "main" {
		return &j.Before.Main
	}
	return j.Before.Alias
}
func validJournal(j *journal) bool {
	if j.Format != 1 || !transactionID.MatchString(j.ID) || (j.Phase != "prepared" && j.Phase != "committed") || (j.Before != nil && !validManifest(j.Before)) || !versionToken.MatchString(j.Identity.Version) || !identityToken.MatchString(j.Identity.Commit) {
		return false
	}
	if j.Before != nil && j.Before.Alias != nil && !j.Alias {
		return false
	}
	if j.Phase == "committed" {
		if !validManifest(j.After) || j.After.Identity != j.Identity || j.After.Main != j.Stages["main"] {
			return false
		}
		if j.Alias {
			if j.After.Alias == nil || *j.After.Alias != j.Stages["alias"] {
				return false
			}
		} else if j.After.Alias != nil {
			return false
		}
	}
	for target, p := range j.Stages {
		if target != "main" && (target != "alias" || !j.Alias) {
			return false
		}
		if p.IndexHigh == 0 && p.IndexLow == 0 {
			return false
		}
		if p.SHA256 != "" && !validProof(p) {
			return false
		}
	}
	return j.Stages != nil
}
func (e *engine) cleanupRetired(ctx context.Context, m *manifest) error {
	keep := []retired{}
	for _, old := range m.Retired {
		if err := e.removeOwned(ctx, old.Name, old.Proof, true); err != nil {
			if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				return err
			}
			keep = append(keep, old)
		}
	}
	m.Retired = keep
	return e.writeJSON(ctx, manifestName, m)
}
func (e *engine) recover(ctx context.Context) error {
	var j journal
	exists, err := e.readJSON(journalName, &j)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !validJournal(&j) {
		return errors.New("invalid installation journal; artifacts retained")
	}
	if j.Phase == "committed" {
		if err = e.require("devtools.exe", j.After.Main); err != nil {
			return err
		}
		if j.After.Alias != nil {
			if err = e.require("dvt.exe", *j.After.Alias); err != nil {
				return err
			}
		}
		if err = e.cleanupRetired(ctx, j.After); err != nil {
			return err
		}
		return e.root.RemoveFile(ctx, journalName)
	}
	// Restore observed names, including a crash between any rename and journal update.
	for _, target := range j.targets() {
		name, backup := executableName(target), oldName(j.ID, target)
		old := j.oldProof(target)
		current, present, err := e.inspect(name)
		if err != nil {
			return err
		}
		if present {
			if old != nil && current == *old { // move has not happened, or this target was already restored
			} else if candidate, ok := j.Stages[target]; ok && validProof(candidate) && current == candidate {
				if err = e.root.RemoveFile(ctx, name); err != nil {
					return err
				}
				present = false
			} else {
				return errors.New("unknown installed executable during recovery; artifacts retained")
			}
		}
		saved, hasBackup, err := e.inspect(backup)
		if err != nil {
			return err
		}
		if hasBackup {
			if old == nil || saved != *old || present {
				return errors.New("ambiguous executable backup; artifacts retained")
			}
			if err = e.root.PublishFile(ctx, backup, name, false); err != nil {
				return err
			}
			present = true
		}
		if old != nil && !present {
			return errors.New("original executable missing; artifacts retained")
		}
		candidate, hasProof := j.Stages[target]
		if hasProof {
			if err = e.removeOwned(ctx, stageName(j.ID, target), candidate, false); err != nil {
				return err
			}
		} else {
			if _, exists, err := e.inspect(stageName(j.ID, target)); err != nil {
				return err
			} else if exists {
				return errors.New("unconfirmed staged file; artifacts retained")
			}
		}
	}
	if j.Before == nil {
		if err = e.root.RemoveFile(ctx, manifestName); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err = e.writeJSON(ctx, manifestName, j.Before); err != nil {
		return err
	}
	return e.root.RemoveFile(ctx, journalName)
}
func (e *engine) stage(ctx context.Context, j *journal, target string, body []byte) error {
	file, err := e.root.OpenFile(stageName(j.ID, target), os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	// Persist the new file's identity before writing; partial copies remain recognizable.
	p, err := fileIdentity(file.File)
	if err == nil {
		j.Stages[target] = p
		err = e.writeJSON(ctx, journalName, j)
	}
	if err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	p.SHA256 = checksum(body)
	j.Stages[target] = p
	if err = e.writeJSON(ctx, journalName, j); err != nil {
		return err
	}
	return e.checkpoint("staged_" + target)
}
func (e *engine) transact(ctx context.Context, options Options, body []byte, want identity, before *manifest) (*Result, error) {
	aliasStatus := "created"
	ownAlias := before != nil && before.Alias != nil
	if ownAlias {
		aliasStatus = "unchanged"
	} else {
		_, err := os.Lstat(filepath.Join(e.directory, "dvt.exe"))
		exists := err == nil
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		_, external := process.LookPath("dvt", e.directory, os.Environ())
		if exists || options.ExternalAlias || external == nil {
			aliasStatus = "skipped_conflict"
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	j := journal{Format: 1, ID: hex.EncodeToString(random[:]), Phase: "prepared", Before: before, Identity: want, Alias: aliasStatus != "skipped_conflict", Stages: map[string]proof{}}
	if err := e.writeJSON(ctx, journalName, j); err != nil {
		return nil, err
	}
	rollback := func(cause error) (*Result, error) { // Recovery must remain possible after caller cancellation.
		restore, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := e.recover(restore); err != nil {
			return nil, fmt.Errorf("installation failed; recovery artifacts retained: %w", errors.Join(cause, err))
		}
		return nil, cause
	}
	if err := e.checkpoint("prepared"); err != nil {
		return rollback(err)
	}
	for _, target := range j.targets() {
		if err := e.stage(ctx, &j, target, body); err != nil {
			return rollback(err)
		}
	}
	if err := e.smoke(ctx, filepath.Join(e.directory, stageName(j.ID, "main")), want); err != nil {
		return rollback(err)
	}
	next := manifest{Format: 1, Identity: want, Main: j.Stages["main"]}
	if j.Alias {
		alias := j.Stages["alias"]
		next.Alias = &alias
	}
	if before != nil {
		next.Retired = append(next.Retired, before.Retired...)
	}
	for _, target := range j.targets() {
		name := executableName(target)
		if old := j.oldProof(target); old != nil {
			if err := e.require(name, *old); err != nil {
				return rollback(err)
			}
			backup := oldName(j.ID, target)
			if err := e.root.PublishFile(ctx, name, backup, false); err != nil {
				return rollback(err)
			}
			next.Retired = append(next.Retired, retired{backup, *old})
			if err := e.checkpoint("old_" + target); err != nil {
				return rollback(err)
			}
		}
		if err := e.require(stageName(j.ID, target), j.Stages[target]); err != nil {
			return rollback(err)
		}
		if err := e.root.PublishFile(ctx, stageName(j.ID, target), name, false); err != nil {
			return rollback(err)
		}
		if err := e.checkpoint("published_" + target); err != nil {
			return rollback(err)
		}
	}
	if err := e.smoke(ctx, filepath.Join(e.directory, "devtools.exe"), want); err != nil {
		return rollback(err)
	}
	if err := e.writeJSON(ctx, manifestName, next); err != nil {
		return rollback(err)
	}
	if err := e.checkpoint("manifest"); err != nil {
		return rollback(err)
	}
	j.Phase = "committed"
	j.After = &next
	if err := e.writeJSON(ctx, journalName, j); err != nil {
		return rollback(err)
	}
	// Once committed, never roll back a successful published generation.
	if err := e.checkpoint("committed"); err != nil {
		return nil, err
	}
	if err := e.cleanupRetired(ctx, &next); err != nil {
		return nil, err
	}
	if err := e.root.RemoveFile(ctx, journalName); err != nil {
		return nil, err
	}
	return &Result{Action: options.Action, Version: want.Version, Alias: AliasResult{"dvt", aliasStatus}, Changed: true}, nil
}
