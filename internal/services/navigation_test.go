package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestServiceCurrentSeparatesAttemptsFromExecutionsAndRetiredCommands(t *testing.T) {
	now := time.Now().UTC()
	rows := []NavigationRow{}
	for i := 0; i < 312; i++ {
		created := now.Add(time.Duration(i-400) * time.Hour)
		ended := created.Add(time.Minute)
		rows = append(rows, navigationRecord(Record{ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), Profile: "app-a", Directory: "/project", Command: "web", State: "stopped", CreatedAt: created, EndedAt: &ended}))
	}
	active := Record{ID: tasks.ID(), Profile: "app-a", Directory: "/project", Command: "web", State: "running", CreatedAt: now.Add(-time.Hour)}
	rows = append(rows, navigationRecord(active))
	attempt := Attempt{ID: tasks.ID(), Action: "restart", Profile: "app-a", Directory: "/project", Command: "web", State: "failed", Reason: "requirements_failed", CreatedAt: now, UpdatedAt: now}
	declarations := []ServiceDeclaration{{Profile: "app-a", Directory: "/project", Command: "web", Served: true}, {Profile: "app-a", Directory: "/project", Command: "api", Served: true}, {Profile: "app-a", Directory: "/project", Command: "check", Served: false}}
	current := summarizeServices(rows, []Attempt{attempt}, declarations, NavigationSelection{})
	if len(current) != 2 {
		t.Fatal(current)
	}
	web := current[0]
	if web.Command != "web" || web.CurrentState != "running" || web.ActiveCount != 1 || web.LastAttempt == nil || web.LastAttempt.State != "failed" || web.LastAttempt.ExecutionID != "" {
		t.Fatal(web)
	}
	if current[1].Command != "api" || current[1].CurrentState != "not_started" {
		t.Fatal(current[1])
	}
	// A later real execution resolves the old failed preparation.
	next := active
	next.ID = tasks.ID()
	next.CreatedAt = now.Add(time.Minute)
	rows = append(rows, navigationRecord(next))
	current = summarizeServices(rows, []Attempt{attempt}, nil, NavigationSelection{})
	if len(current) != 1 || current[0].CurrentState != "conflict" || current[0].ActiveCount != 2 || len(current[0].ActiveIDs) != 2 {
		t.Fatal(current)
	}
	// Removed declarations still retain live/uncertain work; resolved closed
	// retired commands live exclusively in history.
	closed := rows[:312]
	if got := summarizeServices(closed, nil, nil, NavigationSelection{}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestFailedRestartPreparationPersistsBesideExistingExecution(t *testing.T) {
	root, dir := privateTempDir(t), privateTempDir(t)
	s := Store{Data: root}
	id := tasks.ID()
	now := time.Now().UTC()
	r := Record{ID: id, Profile: "app-a", Directory: dir, Command: "web", State: "running", CreatedAt: now.Add(-time.Hour)}
	if e := writePrivate(s.path(id, "record.json"), r); e != nil {
		t.Fatal(e)
	}
	request := tasks.ID()
	_, condition := s.Apply(context.Background(), Request{Action: "restart", ID: id, RequestID: request, RestartPreflight: func(context.Context, Record, RestartPhase) *protocol.Error {
		return protocol.NewError("requirements_failed", "Fixture preflight failure.", 3, nil)
	}})
	if condition == nil || condition.Code != "requirements_failed" {
		t.Fatal(condition)
	}
	attempts, e := s.navigationAttempts()
	if e != nil || len(attempts) != 1 || attempts[0].State != "failed" || attempts[0].ExecutionID != "" || attempts[0].Previous != id {
		t.Fatal(attempts, e)
	}
	after, e := s.read(id)
	if e != nil || after.EndedAt != nil || after.State != "running" {
		t.Fatal(after, e)
	}
	if _, e := os.Stat(filepath.Join(root, "processes", request, "record.json")); !os.IsNotExist(e) {
		t.Fatal("preparation invented an execution", e)
	}
	current := summarizeServices([]NavigationRow{navigationRecord(after)}, attempts, nil, NavigationSelection{})
	if len(current) != 1 || current[0].CurrentState != "running" || current[0].LastAttempt.State != "failed" {
		t.Fatal(current)
	}
}
