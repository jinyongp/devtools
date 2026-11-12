package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// Attempt is operational metadata, not a replay receipt. A failed preparation
// may coexist with a running predecessor and does not claim an execution exists.
type Attempt struct {
	Version     int        `json:"version"`
	ID          string     `json:"request_id"`
	Action      string     `json:"action"`
	Profile     string     `json:"profile"`
	Directory   string     `json:"directory"`
	Instance    string     `json:"instance_id,omitempty"`
	Command     string     `json:"command"`
	Env         string     `json:"env"`
	State       string     `json:"state"`
	Reason      string     `json:"reason"`
	ExecutionID string     `json:"execution_id,omitempty"`
	Previous    string     `json:"previous_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	EndedAt     *time.Time `json:"ended_at"`
	RetiredAt   *time.Time `json:"retired_at,omitempty"`
}

func (s Store) attemptPath(id string) string { return filepath.Join(s.root(), "attempts", id+".json") }
func validAttempt(a Attempt) bool {
	return a.Version == 1 && validID(a.ID) && project.ValidProfile(a.Profile) && filepath.IsAbs(a.Directory) && a.Command != "" && !a.CreatedAt.IsZero() && !a.UpdatedAt.Before(a.CreatedAt)
}

func (s Store) prepareAttempt(q Request) (Attempt, *protocol.Error) {
	var a Attempt
	err := tasks.ReadPrivate(s.attemptPath(q.RequestID), &a)
	if err == nil {
		if !validAttempt(a) {
			return a, storageError()
		}
		return a, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return a, storageErrorAt("read_attempt", err)
	}
	a = Attempt{Version: 1, ID: q.RequestID, Action: q.Action, State: "starting", CreatedAt: time.Now().UTC()}
	a.UpdatedAt = a.CreatedAt
	if q.Action == "start" {
		if r, e := s.read(q.RequestID); e == nil {
			a.Profile, a.Directory, a.Instance, a.Command, a.Env = r.Profile, r.Directory, r.Instance, r.Command, r.Env
		} else if e.Code != "process_not_found" {
			return a, e
		} else {
			p, e := project.Resolve(q.Directory, "")
			if e != nil {
				return a, e
			}
			a.Profile, a.Directory, a.Command = p.Profile, p.Root, q.Command
			a.Env = p.Commands[q.Command].Env
		}
	} else {
		r, e := s.read(q.ID)
		if e != nil {
			return a, e
		}
		a.Profile, a.Directory, a.Instance, a.Command, a.Env, a.Previous = r.Profile, r.Directory, r.Instance, r.Command, r.Env, r.ID
	}
	if q.Env != nil {
		a.Env = *q.Env
	}
	// Older executions can lack a command identity. They remain stoppable, but
	// cannot be attributed to an invented service name by the new summary.
	if a.Command == "" {
		return Attempt{}, nil
	}
	return a, nil
}

func (s Store) saveAttempt(a Attempt, out Result, condition *protocol.Error) *protocol.Error {
	if a.ID == "" {
		return nil
	}
	a.UpdatedAt = time.Now().UTC()
	if out.Item.ID != "" {
		a.ExecutionID = out.Item.ID
		a.Instance = out.Item.Instance
		a.Env = out.Item.Env
		a.State = out.Item.State
		a.Reason = out.Item.Reason
		a.EndedAt = out.Item.EndedAt
	}
	if condition != nil {
		a.State, a.Reason = "failed", condition.Code
		if condition.Code == "launch_outcome_unknown" || condition.Details["effects"] == "unknown" {
			a.State = "unknown"
			a.EndedAt = nil
		} else {
			a.EndedAt = &a.UpdatedAt
		}
	}
	if !validAttempt(a) {
		return storageError()
	}
	if err := writePrivate(s.attemptPath(a.ID), a); err != nil {
		return storageErrorAt("write_attempt", err)
	}
	return nil
}

// RecordPreparationFailure covers initiating CLI/network work that precedes
// Apply. Existing attempts (including immutable completed retries) are untouched.
func (s Store) RecordPreparationFailure(ctx context.Context, q Request, condition *protocol.Error) *protocol.Error {
	if condition == nil || !validID(q.RequestID) || q.Action != "start" && q.Action != "restart" {
		return nil
	}
	release, e := tasks.Lock(ctx, filepath.Join(s.root(), "operation-locks", "request-"+q.RequestID+".lock"))
	if e != nil {
		return storageErrorAt("lock_attempt", e)
	}
	defer release()
	var existing Attempt
	if err := tasks.ReadPrivate(s.attemptPath(q.RequestID), &existing); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return storageErrorAt("read_attempt", err)
	}
	a, err := s.prepareAttempt(q)
	if err != nil {
		return err
	}
	return s.saveAttempt(a, Result{}, condition)
}

func (s Store) navigationAttempts() ([]Attempt, *protocol.Error) {
	items := []Attempt{}
	entries, err := os.ReadDir(filepath.Join(s.root(), "attempts"))
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, storageErrorAt("enumerate_attempts", err)
	}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !strings.HasSuffix(entry.Name(), ".json") || !validID(id) {
			continue
		}
		var a Attempt
		if err := tasks.ReadPrivate(s.attemptPath(id), &a); err != nil {
			return nil, storageErrorAt("read_attempt", err)
		}
		if !validAttempt(a) || a.ID != id {
			return nil, storageError()
		}
		items = append(items, a)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (s Store) StoredAttempts() ([]Attempt, *protocol.Error) { return s.navigationAttempts() }

// Retirement hides resolved history while retaining the original request and
// service summary. It never changes the replay receipt or retries an action.
func (s Store) RetireAttempt(ctx context.Context, id string, now time.Time) error {
	if !validID(id) {
		return errors.New("invalid attempt")
	}
	release, err := tasks.Lock(ctx, filepath.Join(s.root(), "operation-locks", "request-"+id+".lock"))
	if err != nil {
		return err
	}
	defer release()
	var a Attempt
	if err := tasks.ReadPrivate(s.attemptPath(id), &a); err != nil {
		return err
	}
	if !validAttempt(a) || a.ID != id || a.EndedAt == nil {
		return errors.New("unresolved attempt")
	}
	a.RetiredAt = &now
	return tasks.WritePrivate(s.attemptPath(id), a)
}
