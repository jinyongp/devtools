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

func TestWindowsReadinessHelper(t *testing.T) {
	mode := os.Getenv("DEVTOOLS_READINESS_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "ready":
		if os.Getenv("TOKEN") != "canary" {
			os.Exit(9)
		}
		if _, err := os.Stat("marker"); err != nil {
			os.Exit(8)
		}
		os.Exit(0)
	case "exit":
		os.Exit(7)
	case "wait":
		time.Sleep(20 * time.Second)
		os.Exit(0)
	}
	os.Exit(10)
}
func TestWindowsReadinessUsesPreparedContext(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := privateTempDir(t)
	if err = fixtureWriteFile(filepath.Join(dir, "marker"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	probe := project.ReadyProbe{Exec: []string{exe, "-test.run=^TestWindowsReadinessHelper$"}}
	result := runProbe(context.Background(), probe, dir, []string{"DEVTOOLS_READINESS_HELPER=ready", "TOKEN=canary"})
	if !result.Ready || result.ExitCode == nil || *result.ExitCode != 0 || result.CheckedAt.IsZero() {
		t.Fatal(result)
	}
	result = runProbe(context.Background(), probe, dir, []string{"DEVTOOLS_READINESS_HELPER=exit"})
	if result.Ready || result.ExitCode == nil || *result.ExitCode != 7 {
		t.Fatal(result)
	}
	probe.Exec = []string{filepath.Join(dir, "missing.exe")}
	if result = runProbe(context.Background(), probe, dir, nil); result.Reason != "probe_execution_failed" {
		t.Fatal(result)
	}
}
func TestWindowsReadinessDeadlineAndEndedExecution(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe := project.ReadyProbe{Exec: []string{exe, "-test.run=^TestWindowsReadinessHelper$"}, Timeout: "20ms"}
	start := time.Now()
	result := runProbe(context.Background(), probe, privateTempDir(t), []string{"DEVTOOLS_READINESS_HELPER=wait"})
	if result.Ready || result.Reason != "probe_timeout" || time.Since(start) > 4*time.Second {
		t.Fatal(result)
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
