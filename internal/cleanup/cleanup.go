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
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retention"
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
	Plan   string      `json:"plan"`
	IDs    []string    `json:"ids"`
	Items  []candidate `json:"items,omitempty"`
	Done   bool        `json:"done"`
	Result Result      `json:"result"`
}

type logStampOwner struct {
	Mode       string `json:"mode"`
	Profile    string `json:"profile"`
	EndedAt    string `json:"ended_at,omitempty"`
	Capture    *bool  `json:"capture_logs,omitempty"`
	ArchiveID  string `json:"archive_id,omitempty"`
	ArchivedAt string `json:"archived_at,omitempty"`
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
	exit := 3
	switch code {
	case "invalid_argument":
		exit = 2
	case "storage_error":
		exit = 1
	}
	return protocol.NewError(code, "Cleanup condition changed or could not be satisfied. Refresh the preview.", exit, nil)
}
func write(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return maintenance.Write(path, b)
}
func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

func recordLogOwner(record services.Record) services.ArchivedOwner {
	capture := record.Capture
	return services.ArchivedOwner{Profile: record.Profile, EndedAt: record.EndedAt, Capture: &capture}
}

func logOwnerStamp(owner services.ArchivedOwner, payload []byte) (string, bool) {
	var stampOwner logStampOwner
	if owner.Legacy {
		if !project.ValidProfile(owner.Profile) || !validID(owner.ArchiveID) || owner.ArchivedAt.IsZero() {
			return "", false
		}
		stampOwner = logStampOwner{
			Mode:       "legacy_completed_process",
			Profile:    owner.Profile,
			ArchiveID:  owner.ArchiveID,
			ArchivedAt: owner.ArchivedAt.UTC().Format(time.RFC3339Nano),
		}
	} else {
		if !project.ValidProfile(owner.Profile) || owner.EndedAt == nil || owner.Capture == nil {
			return "", false
		}
		capture := *owner.Capture
		stampOwner = logStampOwner{
			Mode:    "record",
			Profile: owner.Profile,
			EndedAt: owner.EndedAt.UTC().Format(time.RFC3339Nano),
			Capture: &capture,
		}
	}
	encoded, err := json.Marshal(stampOwner)
	if err != nil {
		return "", false
	}
	h := sha256.New()
	_, _ = h.Write(encoded)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(payload)
	return fmt.Sprintf("%x", h.Sum(nil)), true
}

func logOwnerExpired(owner services.ArchivedOwner, now time.Time) bool {
	if owner.Legacy {
		return true
	}
	return owner.Capture != nil && *owner.Capture && owner.EndedAt != nil && now.Sub(*owner.EndedAt) > retention.RawProcessLogAge
}

func (e Engine) executionIDFromProcessSource(path string) string {
	dir := filepath.Dir(path)
	if filepath.Dir(dir) != filepath.Join(e.Data, "processes") {
		return ""
	}
	id := filepath.Base(dir)
	if !validID(id) {
		return ""
	}
	return id
}

func removeAndSync(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

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
		return i.Source == profilekey.LegacyPath(filepath.Join(e.Data, "tasks"), i.Profile) || i.Source == profilekey.CanonicalPath(filepath.Join(e.Data, "tasks"), i.Profile)
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
	cutoff := now.Add(-retention.CleanupCandidateAge)
	addWithStamp := func(kind, p, path string, b []byte, stamp string) {
		items = append(items, candidate{Item: Item{ID: tasks.ID(), Kind: kind, Profile: p, Source: path, Bytes: int64(len(b))}, Stamp: stamp})
	}
	add := func(kind, p, path string, b []byte) {
		addWithStamp(kind, p, path, b, digest(b))
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
	taskDirectory := filepath.Join(e.Data, "tasks")
	profiles, err := profilekey.Enumerate(taskDirectory, "tasks")
	if err != nil {
		return nil, fail("storage_error")
	}
	for _, p := range profiles {
		if profile != "" && profile != p {
			continue
		}
		snapshot, err := e.taskSnapshot(p)
		if err != nil {
			return nil, fail("storage_error")
		}
		if snapshot.Data != nil && tasks.ArchiveReady(snapshot.Data, p, cutoff) {
			addWithStamp("completed_tasks", p, snapshot.Source, snapshot.Data, snapshot.Digest)
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
		if r.Capture && now.Sub(*r.EndedAt) > retention.RawProcessLogAge {
			path := filepath.Join(base, "output.log")
			b, exists, logErr := manager.LogSnapshot(ctx, r.ID)
			if logErr != nil {
				return nil, logErr
			}
			if exists {
				stamp, ok := logOwnerStamp(recordLogOwner(r), b)
				if !ok {
					return nil, fail("storage_error")
				}
				addWithStamp("expired_log", r.Profile, path, b, stamp)
			}
		}
	}
	archived, pe := manager.ArchivedExecutions(profile)
	if pe != nil {
		return nil, pe
	}
	for _, archivedExecution := range archived {
		if !logOwnerExpired(archivedExecution.Owner, now) {
			continue
		}
		path := filepath.Join(e.Data, "processes", archivedExecution.ID, "output.log")
		b, exists, logErr := manager.LogSnapshot(ctx, archivedExecution.ID)
		if logErr != nil {
			return nil, logErr
		}
		if !exists {
			continue
		}
		stamp, ok := logOwnerStamp(archivedExecution.Owner, b)
		if !ok {
			return nil, fail("storage_error")
		}
		addWithStamp("expired_log", archivedExecution.Owner.Profile, path, b, stamp)
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
			if n < retention.MinimumBackupCopies || !f.at.Before(cutoff) {
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
	s := snapshot{ID: tasks.ID(), Expires: time.Now().UTC().Add(retention.CleanupPreviewTTL), Profile: profile, Items: items}
	if write(filepath.Join(e.Cache, "cleanup", s.ID+".json"), s) != nil {
		return out, fail("storage_error")
	}
	out = Plan{ID: s.ID, Expires: s.Expires, Items: []Item{}}
	for _, c := range items {
		out.Items = append(out.Items, c.Item)
	}
	return out, nil
}

func mergeArchive(result *Result, archive Archive) *protocol.Error {
	for index := range result.Archives {
		if result.Archives[index].ID != archive.ID {
			continue
		}
		if result.Archives[index].Item != archive.Item {
			return fail("storage_error")
		}
		result.Archives[index] = archive
		return nil
	}
	result.Archives = append(result.Archives, archive)
	return nil
}

func (e Engine) validateProgress(selected []candidate, result Result) *protocol.Error {
	allowed := map[string]Item{}
	for _, candidate := range selected {
		allowed[candidate.ID] = candidate.Item
	}
	seen := map[string]bool{}
	for _, archive := range result.Archives {
		item, ok := allowed[archive.ID]
		if !ok || item != archive.Item || seen[archive.ID] {
			return fail("storage_error")
		}
		seen[archive.ID] = true
	}
	return nil
}
func sameCandidateIDs(items []candidate, ids []string) bool {
	if len(items) != len(ids) {
		return false
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.ID)
	}
	sort.Strings(got)
	for i := range ids {
		if got[i] != ids[i] {
			return false
		}
	}
	return true
}

func (e Engine) selectedFromSnapshot(plan snapshot, ids []string) ([]candidate, *protocol.Error) {
	byID := map[string]candidate{}
	for _, c := range plan.Items {
		if _, exists := byID[c.ID]; exists {
			return nil, fail("storage_error")
		}
		byID[c.ID] = c
	}
	selected := make([]candidate, 0, len(ids))
	for _, id := range ids {
		c, ok := byID[id]
		if !ok {
			return nil, fail("invalid_argument")
		}
		if !e.validItem(c.Item) || c.Stamp == "" {
			return nil, fail("invalid_argument")
		}
		selected = append(selected, c)
	}
	sort.Slice(selected, func(i, j int) bool {
		left, right := selected[i].Kind+":"+selected[i].Source, selected[j].Kind+":"+selected[j].Source
		if left == right {
			return selected[i].ID < selected[j].ID
		}
		return left < right
	})
	return selected, nil
}

func (e Engine) archivedCandidate(c candidate) (Archive, []byte, bool, *protocol.Error) {
	var archive Archive
	err := tasks.ReadPrivate(e.archivePath(c.ID, "entry.json"), &archive)
	if errors.Is(err, os.ErrNotExist) {
		return archive, nil, false, nil
	}
	if err != nil {
		return archive, nil, false, fail("storage_error")
	}
	if archive.Item != c.Item || archive.RestoredAt != nil || archive.PurgedAt != nil {
		return archive, nil, false, fail("revision_conflict")
	}
	payload, err := maintenance.Read(e.archivePath(c.ID, "payload"), 128<<20)
	if err != nil {
		return archive, nil, false, fail("storage_error")
	}
	if c.Kind == "expired_log" {
		executionID := e.executionIDFromProcessSource(c.Source)
		if executionID == "" {
			return archive, nil, false, fail("storage_error")
		}
		owner, ownerErr := (services.Store{Data: e.Data}).LogOwner(executionID)
		if ownerErr != nil {
			return archive, nil, false, fail("revision_conflict")
		}
		stamp, ok := logOwnerStamp(owner, payload)
		if !ok || stamp != c.Stamp {
			return archive, nil, false, fail("revision_conflict")
		}
	} else if digest(payload) != c.Stamp {
		return archive, nil, false, fail("revision_conflict")
	}
	return archive, payload, true, nil
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
	resumed := false
	if readErr := tasks.ReadPrivate(receiptPath, &previous); readErr == nil {
		resumed = true
		if previous.Plan != planID || strings.Join(previous.IDs, ",") != strings.Join(ids, ",") {
			return out, fail("request_conflict")
		}
		if previous.Done {
			previous.Result.Replayed = true
			return previous.Result, nil
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return out, fail("storage_error")
	}

	var selected []candidate
	if resumed {
		selected = append([]candidate{}, previous.Items...)
		if len(selected) == 0 {
			var plan snapshot
			if readErr := tasks.ReadPrivate(filepath.Join(e.Cache, "cleanup", planID+".json"), &plan); readErr != nil || plan.ID != planID {
				return out, fail("revision_conflict")
			}
			selected, err = e.selectedFromSnapshot(plan, ids)
			if err != nil {
				return out, err
			}
			previous.Items = append([]candidate{}, selected...)
			if write(receiptPath, previous) != nil {
				return out, fail("storage_error")
			}
		} else {
			if !sameCandidateIDs(selected, ids) {
				return out, fail("storage_error")
			}
			for _, c := range selected {
				if !e.validItem(c.Item) {
					return out, fail("storage_error")
				}
			}
			sort.Slice(selected, func(i, j int) bool {
				left, right := selected[i].Kind+":"+selected[i].Source, selected[j].Kind+":"+selected[j].Source
				if left == right {
					return selected[i].ID < selected[j].ID
				}
				return left < right
			})
		}
		out = previous.Result
		if out.Archives == nil {
			out.Archives = []Archive{}
		}
		if progressErr := e.validateProgress(selected, out); progressErr != nil {
			return Result{Archives: []Archive{}}, progressErr
		}
		out.Replayed = true
	} else {
		var plan snapshot
		if tasks.ReadPrivate(filepath.Join(e.Cache, "cleanup", planID+".json"), &plan) != nil || plan.ID != planID || time.Now().After(plan.Expires) {
			return out, fail("preview_expired")
		}
		selected, err = e.selectedFromSnapshot(plan, ids)
		if err != nil {
			return out, err
		}
		fresh, scanErr := e.scan(ctx, plan.Profile)
		if scanErr != nil {
			return out, scanErr
		}
		current := map[string]candidate{}
		for _, c := range fresh {
			current[c.Kind+":"+c.Source] = c
		}
		for _, c := range selected {
			if _, _, archived, archiveErr := e.archivedCandidate(c); archiveErr != nil {
				return out, archiveErr
			} else if archived {
				continue
			}
			freshCandidate, ok := current[c.Kind+":"+c.Source]
			if !ok || freshCandidate.Stamp != c.Stamp {
				return out, fail("revision_conflict")
			}
		}
		previous = receipt{Plan: planID, IDs: ids, Items: append([]candidate{}, selected...), Result: out}
		if write(receiptPath, previous) != nil {
			return out, fail("storage_error")
		}
	}

	for _, c := range selected {
		archive, retireErr := e.retire(ctx, c)
		if retireErr != nil {
			return out, retireErr
		}
		if mergeErr := mergeArchive(&out, archive); mergeErr != nil {
			return out, mergeErr
		}
		progress := receipt{Plan: planID, IDs: ids, Items: append([]candidate{}, selected...), Result: out}
		if write(receiptPath, progress) != nil {
			return out, fail("storage_error")
		}
	}
	final := receipt{Plan: planID, IDs: ids, Items: append([]candidate{}, selected...), Done: true, Result: out}
	if write(receiptPath, final) != nil {
		return out, fail("storage_error")
	}
	return out, nil
}
func cleanupProcessError(err *protocol.Error) *protocol.Error {
	if err == nil {
		return nil
	}
	switch err.Code {
	case "revision_conflict", "process_not_found", "process_active":
		return fail("revision_conflict")
	case "canceled":
		return err
	default:
		return fail("storage_error")
	}
}

func (e Engine) retireCompletedProcess(c candidate, archive Archive, already bool) (Archive, *protocol.Error) {
	manager := services.Store{Data: e.Data}
	executionID := e.executionIDFromProcessSource(c.Source)
	if executionID == "" {
		return archive, fail("storage_error")
	}
	body, err := maintenance.Read(c.Source, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		if !already {
			return archive, fail("revision_conflict")
		}
		owner, ownerErr := manager.ArchivedOwner(executionID)
		if ownerErr != nil {
			return archive, cleanupProcessError(ownerErr)
		}
		if owner.Profile != c.Profile {
			return archive, fail("revision_conflict")
		}
		if !owner.Legacy && (owner.ArchiveID != c.ID || !owner.ArchivedAt.Equal(archive.ArchivedAt)) {
			return archive, fail("revision_conflict")
		}
		return archive, nil
	}
	if err != nil || digest(body) != c.Stamp {
		return archive, fail("revision_conflict")
	}
	var record services.Record
	if json.Unmarshal(body, &record) != nil || record.ID != executionID || record.Profile != c.Profile || record.EndedAt == nil {
		return archive, fail("storage_error")
	}
	if !already {
		if maintenance.Write(e.archivePath(c.ID, "payload"), body) != nil || write(e.archivePath(c.ID, "entry.json"), archive) != nil {
			return archive, fail("storage_error")
		}
	}
	if markerErr := manager.WriteArchiveMarker(record, c.ID, archive.ArchivedAt); markerErr != nil {
		return archive, cleanupProcessError(markerErr)
	}
	if err := removeAndSync(c.Source); err != nil {
		return archive, fail("storage_error")
	}
	return archive, nil
}

func (e Engine) retireExpiredLog(ctx context.Context, c candidate, archive Archive, already bool) (Archive, *protocol.Error) {
	executionID := e.executionIDFromProcessSource(c.Source)
	if executionID == "" {
		return archive, fail("storage_error")
	}
	manager := services.Store{Data: e.Data}
	exists, retireErr := manager.RetireLog(ctx, executionID, func(body []byte) *protocol.Error {
		owner, ownerErr := manager.LogOwner(executionID)
		if ownerErr != nil {
			return cleanupProcessError(ownerErr)
		}
		stamp, ok := logOwnerStamp(owner, body)
		if !ok || stamp != c.Stamp {
			return fail("revision_conflict")
		}
		if !already {
			if maintenance.Write(e.archivePath(c.ID, "payload"), body) != nil || write(e.archivePath(c.ID, "entry.json"), archive) != nil {
				return fail("storage_error")
			}
		}
		return nil
	})
	if retireErr != nil {
		return archive, retireErr
	}
	if !exists && !already {
		return archive, fail("revision_conflict")
	}
	return archive, nil
}

func (e Engine) retire(ctx context.Context, c candidate) (Archive, *protocol.Error) {
	archive, _, already, archiveErr := e.archivedCandidate(c)
	if archiveErr != nil {
		return archive, archiveErr
	}
	if !already {
		archive = Archive{Item: c.Item, ArchivedAt: time.Now().UTC()}
	}

	switch c.Kind {
	case "completed_tasks":
		return e.retireCompletedTasks(c, archive, already)
	case "completed_process":
		return e.retireCompletedProcess(c, archive, already)
	case "expired_log":
		return e.retireExpiredLog(ctx, c, archive, already)
	case "missing_instance":
		ps := ports.Store{Directory: filepath.Join(e.Data, "ports")}
		err := ps.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			i := st.ByID(c.Source)
			if i == nil && already {
				return false, nil
			}
			if i == nil || !missing(i.Directory) || (services.Store{Data: e.Data}).Active(ctx, i.ID) != nil {
				return false, fail("revision_conflict")
			}
			value := location{Instance: *i, Assignments: []ports.Assignment{}}
			for _, assignment := range st.Assignments {
				if assignment.ID == i.ID {
					if ps.Active(ctx, i.ID, assignment.Name) != nil {
						return false, fail("revision_conflict")
					}
					free, err := ports.Available(assignment.Port)
					if err != nil || !free {
						return false, fail("revision_conflict")
					}
					value.Assignments = append(value.Assignments, assignment)
				}
			}
			body, _ := json.Marshal(value)
			if digest(body) != c.Stamp {
				return false, fail("revision_conflict")
			}
			if !already {
				if maintenance.Write(e.archivePath(c.ID, "payload"), body) != nil || write(e.archivePath(c.ID, "entry.json"), archive) != nil {
					return false, fail("storage_error")
				}
			}
			instances := []ports.Instance{}
			assignments := []ports.Assignment{}
			for _, instance := range st.Instances {
				if instance.ID != c.Source {
					instances = append(instances, instance)
				}
			}
			for _, assignment := range st.Assignments {
				if assignment.ID != c.Source {
					assignments = append(assignments, assignment)
				}
			}
			st.Instances = instances
			st.Assignments = assignments
			return true, nil
		})
		return archive, err
	}

	body, err := maintenance.Read(c.Source, 128<<20)
	if errors.Is(err, os.ErrNotExist) && already {
		return archive, nil
	}
	if err != nil || digest(body) != c.Stamp {
		return archive, fail("revision_conflict")
	}
	if !already {
		if maintenance.Write(e.archivePath(c.ID, "payload"), body) != nil || write(e.archivePath(c.ID, "entry.json"), archive) != nil {
			return archive, fail("storage_error")
		}
	}
	if err := removeAndSync(c.Source); err != nil {
		return archive, fail("storage_error")
	}
	return archive, nil
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
func (e Engine) restoreCompletedProcess(archive Archive, payload []byte) *protocol.Error {
	executionID := e.executionIDFromProcessSource(archive.Source)
	if executionID == "" {
		return fail("storage_error")
	}
	var record services.Record
	if json.Unmarshal(payload, &record) != nil || record.ID != executionID || record.Profile != archive.Profile || record.EndedAt == nil {
		return fail("storage_error")
	}
	existing, err := maintenance.Read(archive.Source, 1<<20)
	if err == nil {
		if digest(existing) != digest(payload) {
			return fail("revision_conflict")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail("storage_error")
	} else if maintenance.Write(archive.Source, payload) != nil {
		return fail("storage_error")
	}
	if markerErr := (services.Store{Data: e.Data}).RemoveArchiveMarker(executionID, archive.ID); markerErr != nil {
		return cleanupProcessError(markerErr)
	}
	return nil
}

func (e Engine) Restore(ctx context.Context, id string) (Archive, bool, *protocol.Error) {
	var a Archive
	if !validID(id) {
		return a, false, fail("invalid_argument")
	}
	release, err := e.lock(ctx)
	if err != nil {
		return a, false, err
	}
	defer release()
	if tasks.ReadPrivate(e.archivePath(id, "entry.json"), &a) != nil || a.ID != id {
		return a, false, fail("archive_not_found")
	}
	if !e.validItem(a.Item) {
		return a, false, fail("invalid_argument")
	}
	if a.PurgedAt != nil {
		return a, false, fail("archive_purged")
	}
	if a.RestoredAt != nil {
		return a, false, nil
	}
	b, er := maintenance.Read(e.archivePath(id, "payload"), 128<<20)
	if er != nil {
		return a, false, fail("storage_error")
	}
	switch a.Kind {
	case "missing_instance":
		var value location
		if json.Unmarshal(b, &value) != nil {
			return a, false, fail("storage_error")
		}
		ps := ports.Store{Directory: filepath.Join(e.Data, "ports")}
		err = ps.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
			if instance := st.ByID(value.Instance.ID); instance != nil {
				present := location{Instance: *instance, Assignments: []ports.Assignment{}}
				for _, assignment := range st.Assignments {
					if assignment.ID == instance.ID {
						present.Assignments = append(present.Assignments, assignment)
					}
				}
				same, _ := json.Marshal(present)
				if digest(same) == digest(b) {
					return false, nil
				}
				return false, fail("revision_conflict")
			}
			if st.Find(value.Instance.Profile, "", value.Instance.Directory) != nil {
				return false, fail("revision_conflict")
			}
			for _, assignment := range value.Assignments {
				free, e := ports.Available(assignment.Port)
				if e != nil || !free {
					return false, fail("revision_conflict")
				}
			}
			st.Instances = append(st.Instances, value.Instance)
			st.Assignments = append(st.Assignments, value.Assignments...)
			return true, nil
		})
		if err != nil {
			return a, false, err
		}
	case "completed_tasks":
		if restoreErr := e.restoreCompletedTasks(a, b); restoreErr != nil {
			return a, false, restoreErr
		}
	case "completed_process":
		if restoreErr := e.restoreCompletedProcess(a, b); restoreErr != nil {
			return a, false, restoreErr
		}
	case "expired_log":
		executionID := e.executionIDFromProcessSource(a.Source)
		if executionID == "" {
			return a, false, fail("storage_error")
		}
		if restoreErr := (services.Store{Data: e.Data}).RestoreLog(ctx, executionID, b); restoreErr != nil {
			return a, false, cleanupProcessError(restoreErr)
		}
	default:
		existing, readErr := maintenance.Read(a.Source, 128<<20)
		if readErr == nil && digest(existing) != digest(b) {
			return a, false, fail("revision_conflict")
		}
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return a, false, fail("storage_error")
		}
		if readErr != nil && maintenance.Write(a.Source, b) != nil {
			return a, false, fail("storage_error")
		}
	}
	now := time.Now().UTC()
	a.RestoredAt = &now
	if write(e.archivePath(id, "entry.json"), a) != nil {
		return a, false, fail("storage_error")
	}
	return a, true, nil
}
func (e Engine) Purge(ctx context.Context, id string) (Archive, bool, *protocol.Error) {
	var a Archive
	if !validID(id) {
		return a, false, fail("invalid_argument")
	}
	release, err := e.lock(ctx)
	if err != nil {
		return a, false, err
	}
	defer release()
	if tasks.ReadPrivate(e.archivePath(id, "entry.json"), &a) != nil || a.ID != id {
		return a, false, fail("archive_not_found")
	}
	if a.PurgedAt != nil {
		return a, false, nil
	}
	if time.Since(a.ArchivedAt) < retention.ArchivePurgeAge {
		return a, false, fail("retention_active")
	}
	if er := os.Remove(e.archivePath(id, "payload")); er != nil && !errors.Is(er, os.ErrNotExist) {
		return a, false, fail("storage_error")
	}
	now := time.Now().UTC()
	a.PurgedAt = &now
	if write(e.archivePath(id, "entry.json"), a) != nil {
		return a, false, fail("storage_error")
	}
	return a, true, nil
}
