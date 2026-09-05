// Package cleanup previews selected local storage retirement with recoverable archives.
package cleanup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Engine struct{ Data, Cache, Config string }
type Item struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Profile string `json:"profile"`
	Source  string `json:"source"`
	Bytes   int64  `json:"bytes"`
}
type candidate struct {
	Item
	Stamp string `json:"stamp"`
}
type Plan struct {
	ID      string    `json:"id"`
	Expires time.Time `json:"expires"`
	Items   []Item    `json:"items"`
}
type snapshot struct {
	ID      string      `json:"id"`
	Expires time.Time   `json:"expires"`
	Profile string      `json:"profile"`
	Items   []candidate `json:"items"`
}
type Archive struct {
	Item
	ArchivedAt time.Time  `json:"archived_at"`
	RestoredAt *time.Time `json:"restored_at"`
	PurgedAt   *time.Time `json:"purged_at"`
}
type Result struct {
	Archives []Archive `json:"archives"`
	Replayed bool      `json:"replayed"`
}
type receipt struct {
	Plan   string   `json:"plan"`
	IDs    []string `json:"ids"`
	Done   bool     `json:"done"`
	Result Result   `json:"result"`
}
type location struct {
	Instance    ports.Instance     `json:"instance"`
	Assignments []ports.Assignment `json:"assignments"`
}

func validID(id string) bool {
	b, e := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return e == nil && len(b) == 16 && len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-'
}
func fail(code string) *protocol.Error {
	return protocol.NewError(code, "Cleanup condition changed or could not be satisfied. Refresh the preview.", 3, nil)
}
func write(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return maintenance.Write(path, b)
}
func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func (e Engine) archivePath(id, name string) string {
	return filepath.Join(e.Data, "archives", id, name)
}
func (e Engine) validItem(i Item) bool {
	if !validID(i.ID) || i.Profile != "" && !project.ValidProfile(i.Profile) {
		return false
	}
	switch i.Kind {
	case "missing_instance":
		b, err := hex.DecodeString(i.Source)
		return err == nil && len(b) == 16
	case "completed_tasks":
		return i.Source == filepath.Join(e.Data, "tasks", hex.EncodeToString([]byte(i.Profile))+".json")
	case "completed_process", "expired_log":
		name := "record.json"
		if i.Kind == "expired_log" {
			name = "output.log"
		}
		dir := filepath.Dir(i.Source)
		return filepath.Base(i.Source) == name && validID(filepath.Base(dir)) && filepath.Dir(dir) == filepath.Join(e.Data, "processes")
	case "expired_query":
		dir := filepath.Dir(i.Source)
		return (dir == filepath.Join(e.Cache, "task-queries") || dir == filepath.Join(e.Cache, "dashboard", "queries")) && validID(strings.TrimSuffix(filepath.Base(i.Source), ".json")) && strings.HasSuffix(i.Source, ".json")
	case "old_backup":
		var c struct {
			Directory string `json:"directory"`
			Recipient string `json:"recipient"`
		}
		return tasks.ReadPrivate(filepath.Join(e.Config, "backup.json"), &c) == nil && filepath.Dir(i.Source) == c.Directory && strings.HasPrefix(filepath.Base(i.Source), "devtools-") && strings.HasSuffix(i.Source, ".age")
	}
	return false
}
func (e Engine) lock(ctx context.Context) (func(), *protocol.Error) {
	a, err := (services.Store{Data: e.Data}).Lock(ctx)
	if err != nil {
		return nil, fail("storage_error")
	}
	b, err := maintenance.Acquire(ctx, e.Data)
	if err != nil {
		a()
		return nil, fail("storage_error")
	}
	return func() { b(); a() }, nil
}
func (e Engine) scan(ctx context.Context, profile string) ([]candidate, *protocol.Error) {
	items := []candidate{}
	now := time.Now()
	cutoff := now.Add(-30 * 24 * time.Hour)
	add := func(kind, p, path string, b []byte) {
		items = append(items, candidate{Item: Item{ID: tasks.ID(), Kind: kind, Profile: p, Source: path, Bytes: int64(len(b))}, Stamp: digest(b)})
	}
	// Query snapshots are disposable and have explicit expiry timestamps.
	for _, dir := range []string{filepath.Join(e.Cache, "task-queries"), filepath.Join(e.Cache, "dashboard", "queries")} {
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fail("storage_error")
		}
		for _, entry := range entries {
			if !validID(strings.TrimSuffix(entry.Name(), ".json")) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			b, err := maintenance.Read(path, 128<<20)
			if err != nil {
				continue
			}
			var value struct {
				Expires time.Time `json:"expires"`
			}
			if json.Unmarshal(b, &value) == nil && !value.Expires.IsZero() && value.Expires.Before(now) {
				add("expired_query", "", path, b)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(e.Data, "tasks"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fail("storage_error")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		p, err := hex.DecodeString(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || !project.ValidProfile(string(p)) || profile != "" && profile != string(p) {
			continue
		}
		path := filepath.Join(e.Data, "tasks", entry.Name())
		b, err := maintenance.Read(path, 128<<20)
		if err != nil {
			return nil, fail("storage_error")
		}
		if tasks.ArchiveReady(b, string(p), cutoff) {
			add("completed_tasks", string(p), path, b)
		}
	}
	manager := services.Store{Data: e.Data}
	records, pe := manager.List(ctx, profile)
	if pe != nil {
		return nil, pe
	}
	for _, r := range records {
		if r.EndedAt == nil {
			continue
		}
		if manager.Active(ctx, r.Instance) != nil {
			continue
		}
		base := filepath.Join(e.Data, "processes", r.ID)
		if r.EndedAt.Before(cutoff) {
			b, err := maintenance.Read(filepath.Join(base, "record.json"), 1<<20)
			if err == nil {
				add("completed_process", r.Profile, filepath.Join(base, "record.json"), b)
			}
		}
		if r.Capture && now.Sub(*r.EndedAt) > 7*24*time.Hour {
			path := filepath.Join(base, "output.log")
			b, err := maintenance.Read(path, 1<<20)
			if err == nil {
				add("expired_log", r.Profile, path, b)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, fail("storage_error")
			}
		}
	}
	var config struct {
		Directory string `json:"directory"`
		Recipient string `json:"recipient"`
	}
	if profile == "" && tasks.ReadPrivate(filepath.Join(e.Config, "backup.json"), &config) == nil && filepath.IsAbs(config.Directory) {
		entries, err := os.ReadDir(config.Directory)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fail("storage_error")
		}
		type backupFile struct {
			path string
			at   time.Time
		}
		files := []backupFile{}
		for _, entry := range entries {
			stem := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "devtools-"), ".age")
			if !strings.HasPrefix(entry.Name(), "devtools-") || !strings.HasSuffix(entry.Name(), ".age") || !validID(stem) {
				continue
			}
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() {
				files = append(files, backupFile{filepath.Join(config.Directory, entry.Name()), info.ModTime()})
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
		for n, f := range files {
			if n < 3 || !f.at.Before(cutoff) {
				continue
			}
			b, err := maintenance.Read(f.path, 128<<20)
			if err == nil {
				add("old_backup", "", f.path, b)
			}
		}
	}
	ps := ports.Store{Directory: filepath.Join(e.Data, "ports")}
	st, pe := ps.Read()
	if pe != nil {
		return nil, pe
	}
	for _, i := range st.Instances {
		if profile != "" && i.Profile != profile {
			continue
		}
		if !missing(i.Directory) || manager.Active(ctx, i.ID) != nil {
			continue
		}
		value := location{Instance: i, Assignments: []ports.Assignment{}}
		safe := true
		for _, a := range st.Assignments {
			if a.ID != i.ID {
				continue
			}
			if ps.Active(ctx, i.ID, a.Name) != nil {
				safe = false
				break
			}
			free, err := ports.Available(a.Port)
			if err != nil || !free {
				safe = false
				break
			}
			value.Assignments = append(value.Assignments, a)
		}
		if safe {
			b, _ := json.Marshal(value)
			add("missing_instance", i.Profile, i.ID, b)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Kind+items[i].Source < items[j].Kind+items[j].Source })
	return items, nil
}
func missing(path string) bool { _, e := os.Stat(path); return errors.Is(e, os.ErrNotExist) }
func (e Engine) Preview(ctx context.Context, profile string) (Plan, *protocol.Error) {
	var out Plan
	if profile != "" && !project.ValidProfile(profile) {
		return out, fail("invalid_argument")
	}
	release, err := e.lock(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	items, err := e.scan(ctx, profile)
	if err != nil {
		return out, err
	}
	s := snapshot{ID: tasks.ID(), Expires: time.Now().UTC().Add(10 * time.Minute), Profile: profile, Items: items}
	if write(filepath.Join(e.Cache, "cleanup", s.ID+".json"), s) != nil {
		return out, fail("storage_error")
	}
	out = Plan{ID: s.ID, Expires: s.Expires, Items: []Item{}}
	for _, c := range items {
		out.Items = append(out.Items, c.Item)
	}
	return out, nil
}
func (e Engine) Apply(ctx context.Context, planID string, ids []string, requestID string) (Result, *protocol.Error) {
	out := Result{Archives: []Archive{}}
	if !validID(planID) || !validID(requestID) || len(ids) == 0 {
		return out, fail("invalid_argument")
	}
	ids = append([]string{}, ids...)
	sort.Strings(ids)
	for n, id := range ids {
		if !validID(id) || n > 0 && ids[n-1] == id {
			return out, fail("invalid_argument")
		}
	}
	release, err := e.lock(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	receiptPath := filepath.Join(e.Data, "cleanup-receipts", requestID+".json")
	var previous receipt
	if er := tasks.ReadPrivate(receiptPath, &previous); er == nil {
		if previous.Plan != planID || strings.Join(previous.IDs, ",") != strings.Join(ids, ",") {
			return out, fail("request_conflict")
		}
		if previous.Done {
			previous.Result.Replayed = true
			return previous.Result, nil
		}
	} else if !errors.Is(er, os.ErrNotExist) {
		return out, fail("storage_error")
	}
	var plan snapshot
	if tasks.ReadPrivate(filepath.Join(e.Cache, "cleanup", planID+".json"), &plan) != nil || plan.ID != planID || time.Now().After(plan.Expires) {
		return out, fail("preview_expired")
	}
	fresh, err := e.scan(ctx, plan.Profile)
	if err != nil {
		return out, err
	}
	current := map[string]candidate{}
	for _, c := range fresh {
		current[c.Kind+":"+c.Source] = c
	}
	selected := []candidate{}
	for _, id := range ids {
		found := false
		for _, c := range plan.Items {
			if c.ID != id {
				continue
			}
			if !e.validItem(c.Item) {
				return out, fail("invalid_argument")
			}
			found = true
			var archived Archive
			if tasks.ReadPrivate(e.archivePath(id, "entry.json"), &archived) == nil {
				if archived.Item != c.Item {
					return out, fail("revision_conflict")
				}
				if _, er := maintenance.Read(e.archivePath(id, "payload"), 128<<20); er != nil {
					return out, fail("storage_error")
				}
				selected = append(selected, c)
				break
			}
			f, ok := current[c.Kind+":"+c.Source]
			if !ok || f.Stamp != c.Stamp {
				return out, fail("revision_conflict")
			}
			selected = append(selected, c)
			break
		}
		if !found {
			return out, fail("invalid_argument")
		}
	}
	if write(receiptPath, receipt{Plan: planID, IDs: ids}) != nil {
		return out, fail("storage_error")
	}
	for _, c := range selected {
		archive, err := e.retire(ctx, c)
		if err != nil {
			return out, err
		}
		out.Archives = append(out.Archives, archive)
	}
	if write(receiptPath, receipt{Plan: planID, IDs: ids, Done: true, Result: out}) != nil {
		return out, fail("storage_error")
	}
	return out, nil
}
func (e Engine) retire(ctx context.Context, c candidate) (Archive, *protocol.Error) {
	var a Archive
	already := tasks.ReadPrivate(e.archivePath(c.ID, "entry.json"), &a) == nil
	if !already {
		a = Archive{Item: c.Item, ArchivedAt: time.Now().UTC()}
	}
	if c.Kind == "missing_instance" {
		ps := ports.Store{Directory: filepath.Join(e.Data, "ports")}
		err := ps.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			i := st.ByID(c.Source)
			if i == nil && already {
				return false, nil
			}
			if i == nil || !missing(i.Directory) || (services.Store{Data: e.Data}).Active(ctx, i.ID) != nil {
				return false, fail("revision_conflict")
			}
			v := location{Instance: *i, Assignments: []ports.Assignment{}}
			for _, p := range st.Assignments {
				if p.ID == i.ID {
					if ps.Active(ctx, i.ID, p.Name) != nil {
						return false, fail("revision_conflict")
					}
					free, err := ports.Available(p.Port)
					if err != nil || !free {
						return false, fail("revision_conflict")
					}
					v.Assignments = append(v.Assignments, p)
				}
			}
			b, _ := json.Marshal(v)
			if digest(b) != c.Stamp {
				return false, fail("revision_conflict")
			}
			if maintenance.Write(e.archivePath(c.ID, "payload"), b) != nil || write(e.archivePath(c.ID, "entry.json"), a) != nil {
				return false, fail("storage_error")
			}
			instances := []ports.Instance{}
			assignments := []ports.Assignment{}
			for _, v := range st.Instances {
				if v.ID != c.Source {
					instances = append(instances, v)
				}
			}
			for _, v := range st.Assignments {
				if v.ID != c.Source {
					assignments = append(assignments, v)
				}
			}
			st.Instances = instances
			st.Assignments = assignments
			return true, nil
		})
		return a, err
	}
	b, er := maintenance.Read(c.Source, 128<<20)
	if errors.Is(er, os.ErrNotExist) && already {
		return a, nil
	}
	if er != nil || digest(b) != c.Stamp {
		return a, fail("revision_conflict")
	}
	if maintenance.Write(e.archivePath(c.ID, "payload"), b) != nil || write(e.archivePath(c.ID, "entry.json"), a) != nil {
		return a, fail("storage_error")
	}
	if os.Remove(c.Source) != nil {
		return a, fail("storage_error")
	}
	return a, nil
}
func (e Engine) Archives() ([]Archive, *protocol.Error) {
	items := []Archive{}
	entries, err := os.ReadDir(filepath.Join(e.Data, "archives"))
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, fail("storage_error")
	}
	for _, entry := range entries {
		if !validID(entry.Name()) {
			continue
		}
		var a Archive
		if tasks.ReadPrivate(e.archivePath(entry.Name(), "entry.json"), &a) != nil {
			return nil, fail("storage_error")
		}
		items = append(items, a)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ArchivedAt.Before(items[j].ArchivedAt) })
	return items, nil
}
func (e Engine) Restore(ctx context.Context, id string) (Archive, *protocol.Error) {
	var a Archive
	if !validID(id) {
		return a, fail("invalid_argument")
	}
	release, err := e.lock(ctx)
	if err != nil {
		return a, err
	}
	defer release()
	if tasks.ReadPrivate(e.archivePath(id, "entry.json"), &a) != nil || a.ID != id {
		return a, fail("archive_not_found")
	}
	if !e.validItem(a.Item) {
		return a, fail("invalid_argument")
	}
	if a.PurgedAt != nil {
		return a, fail("archive_purged")
	}
	if a.RestoredAt != nil {
		return a, nil
	}
	b, er := maintenance.Read(e.archivePath(id, "payload"), 128<<20)
	if er != nil {
		return a, fail("storage_error")
	}
	if a.Kind == "missing_instance" {
		var v location
		if json.Unmarshal(b, &v) != nil {
			return a, fail("storage_error")
		}
		ps := ports.Store{Directory: filepath.Join(e.Data, "ports")}
		err = ps.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			if i := st.ByID(v.Instance.ID); i != nil {
				present := location{Instance: *i, Assignments: []ports.Assignment{}}
				for _, p := range st.Assignments {
					if p.ID == i.ID {
						present.Assignments = append(present.Assignments, p)
					}
				}
				same, _ := json.Marshal(present)
				if digest(same) == digest(b) {
					return false, nil
				}
				return false, fail("revision_conflict")
			}
			if st.Find(v.Instance.Profile, "", v.Instance.Directory) != nil {
				return false, fail("revision_conflict")
			}
			for _, p := range v.Assignments {
				free, e := ports.Available(p.Port)
				if e != nil || !free {
					return false, fail("revision_conflict")
				}
			}
			st.Instances = append(st.Instances, v.Instance)
			st.Assignments = append(st.Assignments, v.Assignments...)
			return true, nil
		})
		if err != nil {
			return a, err
		}
	} else {
		existing, er := maintenance.Read(a.Source, 128<<20)
		if er == nil && digest(existing) != digest(b) {
			return a, fail("revision_conflict")
		}
		if er != nil && !errors.Is(er, os.ErrNotExist) {
			return a, fail("storage_error")
		}
		if er != nil && maintenance.Write(a.Source, b) != nil {
			return a, fail("storage_error")
		}
	}
	now := time.Now().UTC()
	a.RestoredAt = &now
	if write(e.archivePath(id, "entry.json"), a) != nil {
		return a, fail("storage_error")
	}
	return a, nil
}
func (e Engine) Purge(ctx context.Context, id string) (Archive, *protocol.Error) {
	var a Archive
	if !validID(id) {
		return a, fail("invalid_argument")
	}
	release, err := e.lock(ctx)
	if err != nil {
		return a, err
	}
	defer release()
	if tasks.ReadPrivate(e.archivePath(id, "entry.json"), &a) != nil || a.ID != id {
		return a, fail("archive_not_found")
	}
	if a.PurgedAt != nil {
		return a, nil
	}
	if time.Since(a.ArchivedAt) < 30*24*time.Hour {
		return a, fail("retention_active")
	}
	if er := os.Remove(e.archivePath(id, "payload")); er != nil && !errors.Is(er, os.ErrNotExist) {
		return a, fail("storage_error")
	}
	now := time.Now().UTC()
	a.PurgedAt = &now
	if write(e.archivePath(id, "entry.json"), a) != nil {
		return a, fail("storage_error")
	}
	return a, nil
}
