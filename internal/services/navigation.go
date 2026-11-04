package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
)

type NavigationSelection struct {
	Profile, Directory, Instance, Command, Search string
	Mode                                          string
	Env                                           *string
	History                                       bool
}

type ServiceDeclaration struct {
	Profile, Directory, Command, Env, Instance string
	Served                                     bool
}

// NavigationRow is public metadata. Value slots, launcher input, supervisor
// credentials, output payload and original argv belong to explicit detail reads.
type NavigationRow struct {
	Mode          string          `json:"mode,omitempty"`
	Control       *ControlStatus  `json:"control,omitempty"`
	Cleanup       *CleanupStatus  `json:"cleanup,omitempty"`
	Kind          string          `json:"kind"`
	ID            string          `json:"id"`
	ExecutionID   string          `json:"execution_id,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Profile       string          `json:"profile"`
	Directory     string          `json:"directory"`
	Instance      string          `json:"instance_id,omitempty"`
	Command       string          `json:"command"`
	Env           string          `json:"env"`
	Selection     string          `json:"selection,omitempty"`
	InputRevision string          `json:"input_revision,omitempty"`
	State         string          `json:"state"`
	Reason        string          `json:"reason"`
	CreatedAt     *time.Time      `json:"created_at"`
	UpdatedAt     *time.Time      `json:"updated_at"`
	StartedAt     *time.Time      `json:"started_at"`
	EndedAt       *time.Time      `json:"ended_at"`
	CurrentState  string          `json:"current_state,omitempty"`
	ActiveCount   int             `json:"active_count"`
	ActiveIDs     []string        `json:"active_execution_ids,omitempty"`
	Current       []NavigationRow `json:"current,omitempty"`
	LastAttempt   *Attempt        `json:"last_attempt,omitempty"`
	LastChanged   *time.Time      `json:"last_changed"`
	Declared      bool            `json:"declared"`
	Attention     int             `json:"attention"`
}

func recorded(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	copy := t
	return &copy
}
func latestTime(values ...*time.Time) *time.Time {
	var latest *time.Time
	for _, v := range values {
		if v != nil && !v.IsZero() && (latest == nil || v.After(*latest)) {
			latest = v
		}
	}
	return latest
}

func navigationRecord(r Record) NavigationRow {
	r = r.publicContract()
	row := NavigationRow{Kind: "execution", ID: r.ID, ExecutionID: r.ID, Profile: r.Profile, Directory: r.Directory, Instance: r.Instance, Command: r.Command, Env: r.Env, Selection: r.Selection, State: r.State, Reason: r.Reason, CreatedAt: recorded(r.CreatedAt), StartedAt: r.StartedAt, EndedAt: r.EndedAt}
	row.Mode, row.Control, row.Cleanup = r.Mode, &r.Control, &r.Cleanup
	row.UpdatedAt = latestTime(row.CreatedAt, r.StartedAt, r.EndedAt)
	row.LastChanged = row.UpdatedAt
	if r.InputVersion != nil {
		row.InputRevision = r.InputVersion.Identity.Revision
	}
	return row
}

func selectedNavigation(q NavigationSelection, profile, directory, instance, command, env string) bool {
	if q.Profile != "" && q.Profile != profile || q.Directory != "" && !location.Same(q.Directory, directory) || q.Instance != "" && q.Instance != instance || q.Command != "" && q.Command != command || q.Env != nil && *q.Env != env {
		return false
	}
	return q.Search == "" || strings.Contains(strings.ToLower(strings.Join(strings.Fields(command), " ")), strings.ToLower(strings.Join(strings.Fields(q.Search), " ")))
}

func (s Store) Navigation(ctx context.Context, q NavigationSelection, declarations []ServiceDeclaration) ([]NavigationRow, *protocol.Error) {
	stored, e := s.navigationRecords()
	if e != nil {
		return nil, e
	}
	if !q.History && declarations == nil {
		declarations, e = s.navigationDeclarations(q, stored)
		if e != nil {
			return nil, e
		}
	}
	rows := []NavigationRow{}
	canonical := map[string]string{}
	for _, r := range stored {
		if q.Mode != "" && r.publicContract().Mode != q.Mode {
			continue
		}
		if q.History && r.DetailRetired {
			continue
		}
		if !selectedNavigation(q, r.Profile, r.Directory, r.Instance, r.Command, r.Env) {
			continue
		}
		if r.EndedAt == nil {
			live, e := s.statusRecord(ctx, r)
			if e != nil {
				return nil, e
			}
			r = live
		}
		row := navigationRecord(r)
		if value := canonical[row.Directory]; value != "" {
			row.Directory = value
		} else if value, err := location.Canonical(row.Directory); err == nil {
			canonical[row.Directory], row.Directory = value, value
		}
		rows = append(rows, row)
	}
	attempts, e := s.navigationAttempts()
	if e != nil {
		return nil, e
	}
	selected := []Attempt{}
	for _, a := range attempts {
		if selectedNavigation(q, a.Profile, a.Directory, a.Instance, a.Command, a.Env) {
			if value := canonical[a.Directory]; value != "" {
				a.Directory = value
			} else if value, err := location.Canonical(a.Directory); err == nil {
				canonical[a.Directory], a.Directory = value, value
			}
			selected = append(selected, a)
		}
	}
	if q.History {
		executions := map[string]bool{}
		for _, r := range rows {
			executions[r.ID] = true
		}
		for _, a := range selected {
			if a.RetiredAt != nil || a.ExecutionID != "" || executions[a.ID] {
				continue
			}
			rows = append(rows, NavigationRow{Kind: "preparation", ID: a.ID, RequestID: a.ID, Profile: a.Profile, Directory: a.Directory, Instance: a.Instance, Command: a.Command, Env: a.Env, State: a.State, Reason: a.Reason, CreatedAt: recorded(a.CreatedAt), UpdatedAt: recorded(a.UpdatedAt), EndedAt: a.EndedAt, LastChanged: recorded(a.UpdatedAt)})
		}
		return rows, nil
	}
	return summarizeServices(rows, selected, declarations, q), nil
}

// Kept separate from status observation so storage preparation can publish a
// metadata index without contacting supervisors.
func (s Store) navigationRecords() ([]Record, *protocol.Error) {
	items := []Record{}
	entries, err := os.ReadDir(s.root())
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, storageError(err)
	}
	for _, entry := range entries {
		if !validID(entry.Name()) {
			continue
		}
		d, err := readDiskRecord(s.path(entry.Name(), "record.json"))
		if errors.Is(err, os.ErrNotExist) {
			// Explicitly archived records have no active navigation row.
			if _, e := s.ArchivedOwner(entry.Name()); e != nil {
				return nil, e
			}
			continue
		}
		if err != nil {
			return nil, storageError(err)
		}
		if !validRecord(entry.Name(), d.Metadata) {
			return nil, storageError()
		}
		r := d.Metadata
		r.DetailRetired = d.RetiredAt != nil
		items = append(items, r)
	}
	return items, nil
}

func (s Store) StoredMetadata() ([]Record, *protocol.Error) { return s.navigationRecords() }

func serviceKey(profile, directory, command string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(profile+"\x00"+directory+"\x00"+command)))
}

func summarizeServices(records []NavigationRow, attempts []Attempt, declarations []ServiceDeclaration, q NavigationSelection) []NavigationRow {
	type group struct {
		records     []NavigationRow
		attempts    []Attempt
		declaration *ServiceDeclaration
	}
	groups := map[string]*group{}
	get := func(profile, directory, command string) *group {
		key := serviceKey(profile, directory, command)
		g := groups[key]
		if g == nil {
			g = &group{}
			groups[key] = g
		}
		return g
	}
	for _, r := range records {
		g := get(r.Profile, r.Directory, r.Command)
		g.records = append(g.records, r)
	}
	for _, a := range attempts {
		g := get(a.Profile, a.Directory, a.Command)
		g.attempts = append(g.attempts, a)
	}
	for _, d := range declarations {
		if !selectedNavigation(q, d.Profile, d.Directory, d.Instance, d.Command, d.Env) {
			continue
		}
		g := get(d.Profile, d.Directory, d.Command)
		if d.Served || len(g.records) > 0 || len(g.attempts) > 0 {
			copy := d
			g.declaration = &copy
		}
	}
	rows := []NavigationRow{}
	for key, g := range groups {
		sort.Slice(g.records, func(i, j int) bool {
			a, b := g.records[i], g.records[j]
			if a.CreatedAt == nil {
				return b.CreatedAt != nil
			}
			if b.CreatedAt == nil {
				return false
			}
			if a.CreatedAt.Equal(*b.CreatedAt) {
				return a.ID < b.ID
			}
			return a.CreatedAt.Before(*b.CreatedAt)
		})
		row := NavigationRow{Kind: "service", ID: key, State: "not_started", CurrentState: "not_started", Declared: g.declaration != nil, Attention: 2}
		if d := g.declaration; d != nil {
			row.Profile, row.Directory, row.Command, row.Env, row.Instance = d.Profile, d.Directory, d.Command, d.Env, d.Instance
		}
		for _, r := range g.records {
			row.Mode, row.Control, row.Cleanup = r.Mode, r.Control, r.Cleanup
			row.Profile, row.Directory, row.Command, row.Env, row.Instance = r.Profile, r.Directory, r.Command, r.Env, r.Instance
			row.CreatedAt = earliestTime(row.CreatedAt, r.CreatedAt)
			row.LastChanged = latestTime(row.LastChanged, r.LastChanged)
			if r.EndedAt == nil && (r.State == "running" || r.State == "starting" || r.State == "stopping" || r.State == "unknown") {
				row.Current = append(row.Current, r)
				row.ActiveIDs = append(row.ActiveIDs, r.ID)
			}
			at := time.Time{}
			if r.CreatedAt != nil {
				at = *r.CreatedAt
			}
			updated := at
			if r.UpdatedAt != nil {
				updated = *r.UpdatedAt
			}
			a := Attempt{ID: r.ID, Profile: r.Profile, Directory: r.Directory, Instance: r.Instance, Command: r.Command, Env: r.Env, State: r.State, Reason: r.Reason, ExecutionID: r.ID, CreatedAt: at, UpdatedAt: updated, EndedAt: r.EndedAt, Action: "start"}
			if newerAttempt(a, row.LastAttempt) {
				copy := a
				row.LastAttempt = &copy
			}
			row.State, row.Reason = r.State, r.Reason
		}
		for _, a := range g.attempts {
			row.Profile, row.Directory, row.Command = a.Profile, a.Directory, a.Command
			row.CreatedAt = earliestTime(row.CreatedAt, recorded(a.CreatedAt))
			row.LastChanged = latestTime(row.LastChanged, recorded(a.UpdatedAt))
			if newerAttempt(a, row.LastAttempt) {
				copy := a
				row.LastAttempt = &copy
			}
		}
		row.ActiveCount = len(row.Current)
		if row.ActiveCount == 1 {
			row.Mode, row.Control, row.Cleanup = row.Current[0].Mode, row.Current[0].Control, row.Current[0].Cleanup
			row.State, row.Reason, row.Env, row.Instance = row.Current[0].State, row.Current[0].Reason, row.Current[0].Env, row.Current[0].Instance
			row.ExecutionID = row.Current[0].ID
			row.Attention = 1
		}
		if row.ActiveCount > 1 {
			row.Mode, row.Control, row.Cleanup = "", nil, nil
			row.State, row.Reason, row.Attention = "conflict", "multiple_active_executions", 0
		}
		attention := row.LastAttempt != nil && (row.LastAttempt.State == "failed" || row.LastAttempt.State == "unknown" || row.LastAttempt.State == "interrupted")
		if row.ActiveCount == 0 && row.LastAttempt != nil {
			row.State, row.Reason = row.LastAttempt.State, row.LastAttempt.Reason
			row.Env, row.Instance = row.LastAttempt.Env, row.LastAttempt.Instance
		}
		if !row.Declared && row.ActiveCount == 0 && !attention {
			continue
		}
		if attention || row.State == "unknown" || row.State == "interrupted" {
			row.Attention = 0
		}
		row.CurrentState, row.UpdatedAt = row.State, row.LastChanged
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Attention != rows[j].Attention {
			return rows[i].Attention < rows[j].Attention
		}
		if rows[i].Command != rows[j].Command {
			return rows[i].Command < rows[j].Command
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

func earliestTime(a, b *time.Time) *time.Time {
	if a == nil || b != nil && b.Before(*a) {
		return b
	}
	return a
}
func newerAttempt(a Attempt, b *Attempt) bool {
	return b == nil || a.CreatedAt.After(b.CreatedAt) || a.CreatedAt.Equal(b.CreatedAt) && (a.ID > b.ID || a.ID == b.ID && !a.UpdatedAt.Before(b.UpdatedAt))
}
