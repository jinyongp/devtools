package runnerengine

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/storageerr"
)

func TestEngineHelper(t *testing.T) {
	if os.Getenv("DEVTOOLS_REFERENCE_ENGINE_HELPER") != "1" {
		return
	}
	f, err := os.OpenFile(os.Getenv("DEVTOOLS_REFERENCE_EFFECTS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(2)
	}
	_, err = f.WriteString("effect\n")
	if err != nil || f.Sync() != nil || f.Close() != nil {
		os.Exit(2)
	}
	if os.Getenv("DEVTOOLS_REFERENCE_HOLD") == "1" {
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}
func newEngine(t *testing.T, directory string) *Engine {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Dir(directory))
	if err != nil {
		t.Fatal(err)
	}
	directory = filepath.Join(parent, filepath.Base(directory))
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Directory: directory, Program: program, Arguments: []string{"-test.run=^TestEngineHelper$"}}
	if err := e.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestMissingLedgerCannotAuthorizeWork(t *testing.T) {
	e := &Engine{Directory: filepath.Join(t.TempDir(), "absent"), Program: os.Args[0]}
	if err := e.reserve(context.Background(), "execution", "input", "job", "delivery"); !errors.Is(err, ErrUnknown) {
		t.Fatal("missing ledger created new work", err)
	}
	if _, err := os.Stat(e.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("normal operation initialized a ledger", err)
	}
}
func TestEngineDurableDedupAndRestartLookup(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "ledger")
	e := newEngine(t, dir)
	effects := filepath.Join(t.TempDir(), "effects")
	if err := e.reserve(ctx, "execution", "input", "job", "delivery"); err != nil {
		t.Fatal(err)
	}
	if err := e.run(ctx, "input", "job", "delivery", []string{"DEVTOOLS_REFERENCE_ENGINE_HELPER=1", "DEVTOOLS_REFERENCE_EFFECTS=" + effects}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	restarted := newEngine(t, dir)
	r, err := restarted.Lookup(ctx, "job")
	if err != nil || r.State != "finished" || r.ExitCode == nil || *r.ExitCode != 0 {
		t.Fatal("durable result missing", err)
	}
	if err := restarted.reserve(ctx, "another-execution", "other-input", "job", "new-delivery"); !errors.Is(err, ErrUnknown) {
		t.Fatal("job rebound", err)
	}
	if err := e.run(ctx, "input", "job", "delivery", nil, io.Discard, io.Discard); !errors.Is(err, ErrUnknown) {
		t.Fatal("claim reused", err)
	}
	b, err := os.ReadFile(effects)
	if err != nil || string(b) != "effect\n" {
		t.Fatal("duplicate helper effects", err)
	}
	if _, err := restarted.Lookup(ctx, "absent-job"); !errors.Is(err, ErrUnknown) {
		t.Fatal("absent ledger inferred outcome", err)
	}
}
func TestReservedJobAfterRestartNeverRuns(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "ledger")
	e := newEngine(t, dir)
	if err := e.reserve(ctx, "execution", "input", "job", "delivery"); err != nil {
		t.Fatal(err)
	}
	restarted := newEngine(t, dir)
	r, err := restarted.Lookup(ctx, "job")
	if err != nil || r.State != "unknown" {
		t.Fatal("reservation was promoted", err)
	}
	if err := restarted.run(ctx, "input", "job", "delivery", nil, io.Discard, io.Discard); !errors.Is(err, ErrUnknown) {
		t.Fatal("restart recreated authority", err)
	}
}

func TestLookupDuringExecutionReturnsUnresolvedReservation(t *testing.T) {
	e := newEngine(t, filepath.Join(t.TempDir(), "ledger"))
	effects := filepath.Join(t.TempDir(), "effects")
	ctx := context.Background()
	if err := e.reserve(ctx, "execution", "input", "job", "delivery"); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	e.hook = func(phase string) {
		if phase == "started" {
			close(started)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- e.run(ctx, "input", "job", "delivery", []string{"DEVTOOLS_REFERENCE_ENGINE_HELPER=1", "DEVTOOLS_REFERENCE_EFFECTS=" + effects}, io.Discard, io.Discard)
	}()
	<-started
	lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	r, err := e.Lookup(lookupCtx, "job")
	close(release)
	if err != nil || r.State != "unknown" {
		t.Fatal("lookup blocked on engine or inferred state", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("execution completion: %v; deadline_exceeded=%t; storage=%v", err,
			errors.Is(err, context.DeadlineExceeded), storageerr.Details(err, "runnerengine", "publish_file", "ledger"))
	}
}

func TestEngineCrashOwner(t *testing.T) {
	if os.Getenv("DEVTOOLS_ENGINE_CRASH_OWNER") != "1" {
		return
	}
	e := newEngine(t, os.Getenv("DEVTOOLS_ENGINE_LEDGER"))
	ctx := context.Background()
	if err := e.reserve(ctx, "execution", "input", "crash-job", "delivery"); err != nil {
		os.Exit(2)
	}
	e.hook = func(phase string) {
		if phase == "started" {
			time.Sleep(time.Minute)
		}
	}
	err := e.run(ctx, "input", "crash-job", "delivery", []string{"DEVTOOLS_REFERENCE_ENGINE_HELPER=1", "DEVTOOLS_REFERENCE_EFFECTS=" + os.Getenv("DEVTOOLS_ENGINE_EFFECTS")}, io.Discard, io.Discard)
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestEngineStartBeforeResultCrashDoesNotReexecute(t *testing.T) {
	dir, effects := filepath.Join(t.TempDir(), "ledger"), filepath.Join(t.TempDir(), "effects")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(program, "-test.run=^TestEngineCrashOwner$")
	cmd.Env = append(os.Environ(), "DEVTOOLS_ENGINE_CRASH_OWNER=1", "DEVTOOLS_ENGINE_LEDGER="+dir, "DEVTOOLS_ENGINE_EFFECTS="+effects)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, _ := os.ReadFile(effects)
		if string(b) == "effect\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not execute")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	restarted := newEngine(t, dir)
	r, err := restarted.Lookup(context.Background(), "crash-job")
	if err != nil || r.State != "unknown" {
		t.Fatal("crash inferred execution result", err)
	}
	if err := restarted.reserve(context.Background(), "new", "input", "crash-job", "second"); !errors.Is(err, ErrUnknown) {
		t.Fatal("crash resubmitted job", err)
	}
	b, _ := os.ReadFile(effects)
	if string(b) != "effect\n" {
		t.Fatal("crash recovery duplicated effect")
	}
}
