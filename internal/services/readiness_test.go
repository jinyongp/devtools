package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProbeUsesPreparedContext(t *testing.T) {
	dir := privateTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	p := project.ReadyProbe{Exec: []string{"/bin/sh", "-c", `printf '%s' "$TOKEN"; test "$TOKEN" = canary && test -f marker`}}
	r := runProbe(context.Background(), p, dir, []string{"TOKEN=canary"})
	if !r.Ready || r.ExitCode == nil || *r.ExitCode != 0 || r.CheckedAt.IsZero() {
		t.Fatal(r)
	}
	p.Exec = []string{"/bin/sh", "-c", "exit 7"}
	r = runProbe(context.Background(), p, dir, nil)
	if r.Ready || r.ExitCode == nil || *r.ExitCode != 7 {
		t.Fatal(r)
	}
	p.Exec = []string{"/missing/probe"}
	if r = runProbe(context.Background(), p, dir, nil); r.Reason != "probe_execution_failed" {
		t.Fatal(r)
	}
}

func TestProbeDeadlineAndEndedExecution(t *testing.T) {
	p := project.ReadyProbe{Exec: []string{"/bin/sh", "-c", "sleep 20"}, Timeout: "20ms"}
	start := time.Now()
	r := runProbe(context.Background(), p, privateTempDir(t), []string{"PATH=/usr/bin:/bin"})
	if r.Ready || r.Reason != "probe_timeout" || time.Since(start) > 4*time.Second {
		t.Fatal(r)
	}
	s := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	now := time.Now()
	if err := writePrivate(s.path(id, "record.json"), Record{ID: id, Profile: "test", Directory: privateTempDir(t), EndedAt: &now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(context.Background(), id); err == nil || err.Code != "process_not_running" {
		t.Fatal(err)
	}
}
