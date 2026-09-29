// Package services manages detached, named project commands through authenticated supervisors.
package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/archiveproof"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/tasks"
)

type Store struct{ Data string }

func writePrivate(path string, v any) error {
	if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
		return e
	}
	return tasks.WritePrivate(path, v)
}

type Record struct {
	ReadyConfigured bool       `json:"ready_configured"`
	Readiness       *Readiness `json:"readiness,omitempty"`
	ID              string     `json:"id"`
	Profile         string     `json:"profile"`
	Instance        string     `json:"instance_id"`
	Directory       string     `json:"directory"`
	Command         string     `json:"command"`
	Env             string     `json:"env"`
	EnvOverride     *string    `json:"env_override"`
	Capture         bool       `json:"capture_logs"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	ExitCode        *int       `json:"exit_code"`
	Reason          string     `json:"reason"`
	State           string     `json:"state"`
	Previous        string     `json:"previous_id,omitempty"`
}

type archiveMarker struct {
	Version    int       `json:"version"`
	ArchiveID  string    `json:"archive_id"`
	ArchivedAt time.Time `json:"archived_at"`
	Profile    string    `json:"profile"`
	EndedAt    time.Time `json:"ended_at"`
	Capture    bool      `json:"capture_logs"`
}

type ArchivedOwner struct {
	Profile    string
	ArchiveID  string
	ArchivedAt time.Time
	EndedAt    *time.Time
	Capture    *bool
	Legacy     bool
}

type ArchivedExecution struct {
	ID    string
	Owner ArchivedOwner
}

type executionStorageState uint8

const (
	executionMissing executionStorageState = iota
	executionActive
	executionArchived
	executionHole
)

type storedExecution struct {
	State  executionStorageState
	Record Record
	Owner  ArchivedOwner
}
type RestartPhase string

const (
	RestartBeforeStop  RestartPhase = "before-stop"
	RestartBeforeStart RestartPhase = "before-start"
)

type Request struct {
	Action           string                                                      `json:"action"`
	ID               string                                                      `json:"id,omitempty"`
	Directory        string                                                      `json:"directory,omitempty"`
	Command          string                                                      `json:"command,omitempty"`
	Env              *string                                                     `json:"env,omitempty"`
	Capture          *bool                                                       `json:"capture_logs,omitempty"`
	RequestID        string                                                      `json:"request_id"`
	BeforeStart      func(context.Context) *protocol.Error                       `json:"-"`
	RestartPreflight func(context.Context, Record, RestartPhase) *protocol.Error `json:"-"`
}
type Result struct {
	Item     Record `json:"item"`
	Changed  bool   `json:"changed"`
	Replayed bool   `json:"replayed"`
}
type receipt struct {
	Fingerprint string `json:"fingerprint"`
	Result      Result `json:"result"`
	Done        bool   `json:"done"`
}
type control struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Token   string `json:"token"`
}

func failure(code string) *protocol.Error {
	exit := 3
	switch code {
	case "invalid_argument":
		exit = 2
	case "canceled":
		exit = 130
	case "execution_failed":
		exit = 126
	}
	return protocol.NewError(code, "Process operation could not satisfy the requested condition.", exit, nil)
}
func storageError() *protocol.Error {
	return protocol.NewError("io_error", "Cannot access private process storage.", 1, nil)
}
func validID(s string) bool {
	b, e := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return e == nil && len(b) == 16 && len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}
func (s Store) root() string                { return filepath.Join(s.Data, "processes") }
func (s Store) path(id, name string) string { return filepath.Join(s.root(), id, name) }

func validRecord(id string, r Record) bool {
	return r.ID == id && project.ValidProfile(r.Profile) && filepath.IsAbs(r.Directory)
}

func markerOwner(m archiveMarker) ArchivedOwner {
	ended := m.EndedAt
	capture := m.Capture
	return ArchivedOwner{Profile: m.Profile, ArchiveID: m.ArchiveID, ArchivedAt: m.ArchivedAt, EndedAt: &ended, Capture: &capture}
}

func legacyOwner(p archiveproof.Proof) ArchivedOwner {
	return ArchivedOwner{Profile: p.Profile, ArchiveID: p.ArchiveID, ArchivedAt: p.ArchivedAt, Legacy: true}
}

func validMarker(m archiveMarker) bool {
	return m.Version == 1 && validID(m.ArchiveID) && !m.ArchivedAt.IsZero() && project.ValidProfile(m.Profile) && !m.EndedAt.IsZero()
}

func markerMatchesRecord(m archiveMarker, r Record) bool {
	return r.EndedAt != nil && m.Profile == r.Profile && m.EndedAt.Equal(*r.EndedAt) && m.Capture == r.Capture
}

func (s Store) readMarker(id string) (archiveMarker, bool, *protocol.Error) {
	var marker archiveMarker
	err := tasks.ReadPrivate(s.path(id, "record.archive.json"), &marker)
	if errors.Is(err, os.ErrNotExist) {
		return marker, false, nil
	}
	if err != nil || !validMarker(marker) {
		return marker, false, storageError()
	}
	return marker, true, nil
}

func (s Store) inspectLocal(id string) (storedExecution, *protocol.Error) {
	var stored storedExecution
	if !validID(id) {
		return stored, failure("invalid_argument")
	}
	dir := filepath.Join(s.root(), id)
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		stored.State = executionMissing
		return stored, nil
	}
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return stored, storageError()
	}

	var record Record
	recordErr := tasks.ReadPrivate(s.path(id, "record.json"), &record)
	if recordErr != nil && !errors.Is(recordErr, os.ErrNotExist) {
		return stored, storageError()
	}
	marker, hasMarker, markerErr := s.readMarker(id)
	if markerErr != nil {
		return stored, markerErr
	}
	if recordErr == nil {
		if !validRecord(id, record) || hasMarker && !markerMatchesRecord(marker, record) {
			return stored, storageError()
		}
		stored.State = executionActive
		stored.Record = record
		if hasMarker {
			stored.Owner = markerOwner(marker)
		}
		return stored, nil
	}
	if hasMarker {
		stored.State = executionArchived
		stored.Owner = markerOwner(marker)
		return stored, nil
	}
	stored.State = executionHole
	return stored, nil
}

func (s Store) legacyProofs(ids []string) (map[string]archiveproof.Proof, *protocol.Error) {
	proofs, err := archiveproof.CompletedProcesses(s.Data, ids)
	if err != nil {
		return nil, storageError()
	}
	return proofs, nil
}

func (s Store) read(id string) (Record, *protocol.Error) {
	stored, err := s.inspectLocal(id)
	if err != nil {
		return Record{}, err
	}
	switch stored.State {
	case executionActive:
		return stored.Record, nil
	case executionMissing, executionArchived:
		return Record{}, failure("process_not_found")
	case executionHole:
		proofs, proofErr := s.legacyProofs([]string{id})
		if proofErr != nil {
			return Record{}, proofErr
		}
		if _, ok := proofs[id]; ok {
			return Record{}, failure("process_not_found")
		}
		return Record{}, storageError()
	default:
		return Record{}, storageError()
	}
}

// ArchivedOwner returns durable owner metadata for an archived execution.
// Future cleanup markers preserve ended/capture metadata; legacy proofs only
// preserve the fields that old cleanup archives stored durably.
func (s Store) ArchivedOwner(id string) (ArchivedOwner, *protocol.Error) {
	stored, err := s.inspectLocal(id)
	if err != nil {
		return ArchivedOwner{}, err
	}
	switch stored.State {
	case executionArchived:
		return stored.Owner, nil
	case executionHole:
		proofs, proofErr := s.legacyProofs([]string{id})
		if proofErr != nil {
			return ArchivedOwner{}, proofErr
		}
		if proof, ok := proofs[id]; ok {
			return legacyOwner(proof), nil
		}
		return ArchivedOwner{}, storageError()
	case executionMissing:
		return ArchivedOwner{}, failure("process_not_found")
	default:
		return ArchivedOwner{}, failure("process_active")
	}
}

func recordOwner(record Record) ArchivedOwner {
	capture := record.Capture
	return ArchivedOwner{Profile: record.Profile, EndedAt: record.EndedAt, Capture: &capture}
}

// LogOwner resolves the stable owner metadata used by cleanup log candidates.
// Active ended records and future archive markers share the same owner shape;
// pre-marker archived executions use immutable archive proof metadata.
func (s Store) LogOwner(id string) (ArchivedOwner, *protocol.Error) {
	stored, err := s.inspectLocal(id)
	if err != nil {
		return ArchivedOwner{}, err
	}
	switch stored.State {
	case executionActive:
		return recordOwner(stored.Record), nil
	case executionArchived:
		return stored.Owner, nil
	case executionHole:
		proofs, proofErr := s.legacyProofs([]string{id})
		if proofErr != nil {
			return ArchivedOwner{}, proofErr
		}
		if proof, ok := proofs[id]; ok {
			return legacyOwner(proof), nil
		}
		return ArchivedOwner{}, storageError()
	case executionMissing:
		return ArchivedOwner{}, failure("process_not_found")
	default:
		return ArchivedOwner{}, storageError()
	}
}

// ArchivedExecutions enumerates marker-backed and pre-marker archived process
// directories without contacting supervisors.
func (s Store) ArchivedExecutions(profile string) ([]ArchivedExecution, *protocol.Error) {
	items := []ArchivedExecution{}
	if profile != "" && !project.ValidProfile(profile) {
		return nil, failure("invalid_argument")
	}
	entries, err := os.ReadDir(s.root())
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, storageError()
	}
	holes := []string{}
	for _, entry := range entries {
		id := entry.Name()
		if !validID(id) {
			continue
		}
		stored, inspectErr := s.inspectLocal(id)
		if inspectErr != nil {
			return nil, inspectErr
		}
		switch stored.State {
		case executionActive, executionMissing:
			continue
		case executionArchived:
			if profile == "" || stored.Owner.Profile == profile {
				items = append(items, ArchivedExecution{ID: id, Owner: stored.Owner})
			}
		case executionHole:
			holes = append(holes, id)
		default:
			return nil, storageError()
		}
	}
	if len(holes) > 0 {
		proofs, proofErr := s.legacyProofs(holes)
		if proofErr != nil {
			return nil, proofErr
		}
		for _, id := range holes {
			proof, ok := proofs[id]
			if !ok {
				return nil, storageError()
			}
			owner := legacyOwner(proof)
			if profile == "" || owner.Profile == profile {
				items = append(items, ArchivedExecution{ID: id, Owner: owner})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s Store) WriteArchiveMarker(record Record, archiveID string, archivedAt time.Time) *protocol.Error {
	if !validID(record.ID) || !validRecord(record.ID, record) || record.EndedAt == nil || !validID(archiveID) || archivedAt.IsZero() {
		return failure("invalid_argument")
	}
	marker := archiveMarker{
		Version:    1,
		ArchiveID:  archiveID,
		ArchivedAt: archivedAt.UTC(),
		Profile:    record.Profile,
		EndedAt:    record.EndedAt.UTC(),
		Capture:    record.Capture,
	}
	current, err := s.inspectLocal(record.ID)
	if err != nil {
		return err
	}
	if current.State != executionActive || !markerMatchesRecord(marker, current.Record) {
		return failure("revision_conflict")
	}
	if current.Owner.ArchiveID != "" {
		if current.Owner.ArchiveID != archiveID || !current.Owner.ArchivedAt.Equal(marker.ArchivedAt) {
			return failure("revision_conflict")
		}
		return nil
	}
	if err := writePrivate(s.path(record.ID, "record.archive.json"), marker); err != nil {
		return storageError()
	}
	return nil
}

func (s Store) RemoveArchiveMarker(id, archiveID string) *protocol.Error {
	if !validID(id) || !validID(archiveID) {
		return failure("invalid_argument")
	}
	marker, exists, err := s.readMarker(id)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if marker.ArchiveID != archiveID {
		return failure("revision_conflict")
	}
	var record Record
	recordErr := tasks.ReadPrivate(s.path(id, "record.json"), &record)
	if recordErr == nil {
		if !validRecord(id, record) || !markerMatchesRecord(marker, record) {
			return failure("revision_conflict")
		}
	} else if !errors.Is(recordErr, os.ErrNotExist) {
		return storageError()
	}
	path := s.path(id, "record.archive.json")
	if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return storageError()
	}
	dir, openErr := os.Open(filepath.Dir(path))
	if openErr != nil {
		return storageError()
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil || closeErr != nil {
		return storageError()
	}
	return nil
}
func (s Store) rpc(ctx context.Context, id, action string) (Record, error) {
	var c control
	var out Record
	if tasks.ReadPrivate(s.path(id, "control.json"), &c) != nil || c.ID != id || !strings.HasPrefix(c.Address, "http://127.0.0.1:") || len(c.Token) != 64 {
		return out, errors.New("unavailable")
	}
	u, parseErr := url.Parse(c.Address)
	if parseErr != nil {
		return out, errors.New("invalid address")
	}
	port, parseErr := strconv.Atoi(u.Port())
	if parseErr != nil || port < 1 || port > 65535 || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return out, errors.New("invalid address")
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.Address+"/"+action, nil)
	if e != nil {
		return out, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	timeout := time.Second
	if action == "check" {
		timeout = 35 * time.Second
	}
	client := http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, e := client.Do(req)
	if e != nil {
		return out, e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return out, errors.New("unavailable")
	}
	e = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&out)
	if out.ID != id {
		return out, errors.New("identity mismatch")
	}
	return out, e
}
func (s Store) statusRecord(ctx context.Context, r Record) (Record, *protocol.Error) {
	if r.EndedAt != nil {
		return r, nil
	}
	if live, err := s.rpc(ctx, r.ID, "status"); err == nil {
		return live, nil
	}
	lost, err := s.supervisorLost(ctx, r, true)
	if err != nil {
		return r, err
	}
	if lost {
		r.State = "interrupted"
		r.Reason = "supervisor_lost"
		return r, nil
	}
	if r.StartedAt == nil {
		r.State = "starting"
		r.Reason = ""
		return r, nil
	}
	r.State = "unknown"
	r.Reason = "supervisor_unavailable"
	return r, nil
}

func (s Store) Status(ctx context.Context, id string) (Record, *protocol.Error) {
	r, err := s.read(id)
	if err != nil {
		return r, err
	}
	return s.statusRecord(ctx, r)
}

func (s Store) leaseFree(ctx context.Context, id string) bool {
	_, hasMarker, markerErr := s.readLaunchMarker(id)
	if markerErr != nil {
		return false
	}
	available, err := s.leaseAvailable(ctx, id, !hasMarker)
	return err == nil && available
}

func (s Store) storedRecords() ([]Record, *protocol.Error) {
	items := []Record{}
	entries, err := os.ReadDir(s.root())
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, storageError()
	}
	holes := []string{}
	for _, entry := range entries {
		id := entry.Name()
		if !validID(id) {
			continue
		}
		stored, inspectErr := s.inspectLocal(id)
		if inspectErr != nil {
			return nil, inspectErr
		}
		switch stored.State {
		case executionActive:
			items = append(items, stored.Record)
		case executionMissing, executionArchived:
			continue
		case executionHole:
			holes = append(holes, id)
		default:
			return nil, storageError()
		}
	}
	if len(holes) > 0 {
		proofs, proofErr := s.legacyProofs(holes)
		if proofErr != nil {
			return nil, proofErr
		}
		for _, id := range holes {
			if _, ok := proofs[id]; !ok {
				return nil, storageError()
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

func (s Store) List(ctx context.Context, profile string) ([]Record, *protocol.Error) {
	if profile != "" && !project.ValidProfile(profile) {
		return nil, failure("invalid_argument")
	}
	stored, err := s.storedRecords()
	if err != nil {
		return nil, err
	}
	items := []Record{}
	for _, r := range stored {
		if profile != "" && r.Profile != profile {
			continue
		}
		live, statusErr := s.statusRecord(ctx, r)
		if statusErr != nil {
			return nil, statusErr
		}
		if profile == "" || live.Profile == profile {
			items = append(items, live)
		}
	}
	return items, nil
}

// StoredRecords reads process records without contacting live supervisors.
func (s Store) StoredRecords() ([]Record, *protocol.Error) {
	return s.storedRecords()
}

// ProfileNames enumerates profiles from stored process records without
// contacting live supervisors. Catalog-style discovery must stay passive and
// must not turn a profile listing into readiness or control RPC traffic.
func (s Store) ProfileNames() ([]string, *protocol.Error) {
	records, err := s.StoredRecords()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, record := range records {
		names[record.Profile] = true
	}
	items := make([]string, 0, len(names))
	for name := range names {
		items = append(items, name)
	}
	sort.Strings(items)
	return items, nil
}
func (s Store) Active(ctx context.Context, instance string) *protocol.Error {
	items, e := s.List(ctx, "")
	if e != nil {
		return e
	}
	for _, r := range items {
		if r.Instance == instance && ((r.EndedAt == nil && r.State != "interrupted") || !s.leaseFree(ctx, r.ID)) {
			return failure("process_active")
		}
	}
	return nil
}
func (s Store) Lock(ctx context.Context) (func(), error) {
	return tasks.Lock(ctx, filepath.Join(s.root(), "operations.lock"))
}
func (s Store) Apply(ctx context.Context, q Request) (Result, *protocol.Error) {
	var out Result
	if !validID(q.RequestID) {
		return out, failure("invalid_argument")
	}
	if q.Action == "start" && q.ID != "" || q.Action != "start" && (q.Command != "" || q.Directory != "") {
		return out, failure("invalid_argument")
	}
	unlock, e := tasks.Lock(ctx, filepath.Join(s.root(), "operations.lock"))
	if e != nil {
		return out, storageError()
	}
	defer unlock()
	b, _ := json.Marshal(q)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(b))
	receiptPath := filepath.Join(s.root(), "receipts", q.RequestID+".json")
	var old receipt
	receiptExists := false
	if e = tasks.ReadPrivate(receiptPath, &old); e == nil {
		receiptExists = true
		if old.Fingerprint != fingerprint {
			return out, failure("request_conflict")
		}
		if old.Done {
			old.Result.Replayed = true
			return old.Result, nil
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return out, storageError()
	}
	if cleanupErr := s.cleanupLaunchStaging(); cleanupErr != nil {
		return out, cleanupErr
	}
	if !receiptExists {
		if writePrivate(receiptPath, receipt{Fingerprint: fingerprint}) != nil {
			return out, storageError()
		}
	}
	var err *protocol.Error
	switch q.Action {
	case "start":
		out, err = s.start(ctx, q, "")
	case "stop", "restart":
		var r Record
		r, err = s.read(q.ID)
		if err != nil {
			break
		}
		if q.Action == "stop" && (q.Env != nil || q.Capture != nil) {
			err = failure("invalid_argument")
			break
		}
		if q.Action == "restart" && r.EndedAt == nil && q.RestartPreflight != nil {
			err = q.RestartPreflight(ctx, r, RestartBeforeStop)
			if err != nil {
				break
			}
		}
		out, err = s.stop(ctx, r)
		if err != nil {
			break
		}
		if q.Action == "restart" {
			q.Directory = r.Directory
			q.Command = r.Command
			if q.Env == nil {
				q.Env = r.EnvOverride
			}
			if q.Capture == nil {
				q.Capture = &r.Capture
			}
			if q.RestartPreflight != nil {
				legacyBeforeStart := q.BeforeStart
				q.BeforeStart = func(ctx context.Context) *protocol.Error {
					if preflightErr := q.RestartPreflight(ctx, r, RestartBeforeStart); preflightErr != nil {
						return preflightErr
					}
					if legacyBeforeStart != nil {
						return legacyBeforeStart(ctx)
					}
					return nil
				}
			}
			started, startErr := s.start(ctx, q, r.ID)
			if started.Item.ID != "" {
				out.Item = started.Item
			}
			out.Changed = out.Changed || started.Changed
			err = startErr
		}
	default:
		err = failure("invalid_argument")
	}
	if out.Item.ID == "" && old.Result.Item.ID != "" {
		out.Item = old.Result.Item
	}
	out.Changed = out.Changed || old.Result.Changed
	stored := out
	stored.Replayed = false
	if err != nil {
		if writePrivate(receiptPath, receipt{Fingerprint: fingerprint, Result: stored}) != nil {
			return out, storageError()
		}
		out.Replayed = receiptExists
		return out, err
	}
	if writePrivate(receiptPath, receipt{Fingerprint: fingerprint, Result: stored, Done: true}) != nil {
		return out, storageError()
	}
	out.Replayed = receiptExists
	return out, nil
}
func (s Store) start(ctx context.Context, q Request, previous string) (Result, *protocol.Error) {
	var out Result
	// Request identity also names the execution, so retry after a lost receipt
	// observes the same run instead of launching another command.
	if r, e := s.read(q.RequestID); e == nil {
		out.Item = r
		out.Changed = true
		if r.StartedAt == nil && r.EndedAt == nil {
			current, _, reconcileErr := s.terminalizeLost(ctx, r, true)
			out.Item = current
			if reconcileErr != nil {
				return out, reconcileErr
			}
			if current.EndedAt != nil {
				return out, nil
			}
			return out, failure("process_pending")
		}
		return out, nil
	} else if e.Code != "process_not_found" {
		return out, e
	}
	p, e := project.Resolve(q.Directory, "")
	if e != nil {
		return out, e
	}
	c, ok := p.Commands[q.Command]
	if !ok {
		return out, failure("command_not_found")
	}
	env := c.Env
	if q.Env != nil {
		env = *q.Env
		if env != "" && !project.ValidProfile(env) {
			return out, failure("invalid_argument")
		}
	}
	capture := q.Capture != nil && *q.Capture
	if q.BeforeStart != nil {
		list, listErr := s.List(ctx, p.Profile)
		if listErr != nil {
			return out, listErr
		}
		for _, r := range list {
			if r.Directory == p.Root && r.Command == q.Command && r.EndedAt == nil && r.State != "interrupted" {
				if r.State == "unknown" {
					return out, failure("process_unavailable")
				}
				if r.Env != env || r.Capture != capture {
					return out, failure("process_conflict")
				}
				return Result{Item: r}, nil
			}
		}
		if err := q.BeforeStart(ctx); err != nil {
			return out, err
		}
	}
	ps := ports.Store{Directory: filepath.Join(s.Data, "ports")}
	var instance ports.Instance
	e = ps.Update(ctx, func(st *ports.State) (bool, *protocol.Error) {
		i := st.Find(p.Profile, "", p.Root)
		if i != nil {
			instance = *i
			return false, nil
		}
		i, e := st.Register(p.Profile, p.Root)
		if e != nil {
			return false, e
		}
		instance = *i
		return true, nil
	})
	if e != nil {
		return out, e
	}
	if q.BeforeStart == nil {
		list, listErr := s.List(ctx, p.Profile)
		if listErr != nil {
			return out, listErr
		}
		for _, r := range list {
			if r.Instance == instance.ID && r.Command == q.Command && r.EndedAt == nil && r.State != "interrupted" {
				if r.State == "unknown" {
					return out, failure("process_unavailable")
				}
				if r.Env != env || r.Capture != capture {
					return out, failure("process_conflict")
				}
				return Result{Item: r}, nil
			}
		}
	}
	r := Record{ID: q.RequestID, Profile: p.Profile, Instance: instance.ID, Directory: p.Root, Command: q.Command, Env: env, EnvOverride: q.Env, Capture: capture, CreatedAt: time.Now().UTC(), State: "starting", Previous: previous}
	exe, err := os.Executable()
	if err != nil {
		return out, storageError()
	}
	lease, launchErr := s.prepareLaunch(r)
	if launchErr != nil {
		return out, launchErr
	}
	cmd := exec.Command(exe, "__process-serve", s.Data, r.ID, "3")
	cmd.Dir = p.Root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.ExtraFiles = []*os.File{lease.file}
	if err = cmd.Start(); err != nil {
		now := time.Now().UTC()
		r.EndedAt = &now
		r.State = "failed"
		r.Reason = "supervisor_start_failed"
		writeErr := writePrivate(s.path(r.ID, "record.json"), r)
		leaseErr := lease.unlockClose()
		if writeErr != nil || leaseErr != nil {
			return Result{Item: r, Changed: true}, storageError()
		}
		return Result{Item: r, Changed: true}, nil
	}
	_ = lease.closeReference()
	go func() { _ = cmd.Wait() }()
	deadline := time.NewTimer(legacyStartupGrace)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if current, readErr := s.read(r.ID); readErr == nil {
				r = current
			}
			return Result{Item: r, Changed: true}, failure("process_pending")
		case <-deadline.C:
			if current, readErr := s.read(r.ID); readErr == nil {
				r = current
			}
			return Result{Item: r, Changed: true}, failure("process_pending")
		case <-tick.C:
			current, e := s.read(r.ID)
			if e != nil {
				return out, e
			}
			if current.StartedAt != nil || current.EndedAt != nil {
				return Result{Item: current, Changed: true}, nil
			}
		}
	}
}
func (s Store) stop(ctx context.Context, r Record) (Result, *protocol.Error) {
	if r.EndedAt != nil {
		return Result{Item: r}, nil
	}
	if _, e := s.rpc(ctx, r.ID, "stop"); e != nil {
		current, changed, reconcileErr := s.terminalizeLost(ctx, r, false)
		if reconcileErr != nil {
			return Result{}, reconcileErr
		}
		if changed || current.EndedAt != nil {
			return Result{Item: current, Changed: changed}, nil
		}
		return Result{}, failure("process_unavailable")
	}
	wait, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-wait.Done():
			if current, readErr := s.read(r.ID); readErr == nil {
				r = current
			}
			return Result{Item: r, Changed: true}, failure("process_pending")
		case <-tick.C:
			current, e := s.read(r.ID)
			if e != nil {
				return Result{}, e
			}
			if current.EndedAt != nil && s.leaseFree(ctx, r.ID) {
				return Result{Item: current, Changed: true}, nil
			}
		}
	}
}
func (s Store) Logs(ctx context.Context, id string) (string, *protocol.Error) {
	r, e := s.Status(ctx, id)
	if e != nil {
		return "", e
	}
	if !r.Capture {
		return "", failure("logs_disabled")
	}
	if r.EndedAt != nil && time.Since(*r.EndedAt) > retention.RawProcessLogAge {
		return "", failure("logs_expired")
	}
	b, exists, logErr := s.LogSnapshot(ctx, id)
	if logErr != nil {
		return "", logErr
	}
	if !exists {
		return "", nil
	}
	return string(bytes.ToValidUTF8(b, []byte("�"))), nil
}
