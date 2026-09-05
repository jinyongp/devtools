package process

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestEnvironmentAndExecutablePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte("#!/bin/sh\nprintf '%s' \"$VALUE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	env := Environment([]string{"VALUE=old", "VALUE=duplicate", "PATH=/not-used"}, map[string]string{"PATH": dir, "VALUE": "new"})
	exit, err := Execute(context.Background(), []string{"fixture"}, dir, env, nil, &out, &diagnostic)
	if err != nil || exit != 0 || out.String() != "new" {
		t.Fatalf("%d %s %v", exit, &out, err)
	}
	_, err = Execute(context.Background(), []string{"missing"}, dir, env, nil, &out, &diagnostic)
	if err == nil || err.ExitCode != 127 {
		t.Fatal(err)
	}
}

type readyWriter struct {
	once  sync.Once
	ready chan struct{}
}

func (w *readyWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ready) })
	return len(p), nil
}

func TestSignalForwarding(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	ready := &readyWriter{ready: make(chan struct{})}
	done := make(chan int, 1)
	go func() {
		exit, err := Execute(ctx, []string{"/bin/sh", "-c", "trap 'exit 42' TERM; printf ready; while :; do sleep 1; done"}, t.TempDir(), os.Environ(), nil, ready, &bytes.Buffer{})
		if err != nil {
			done <- -1
			return
		}
		done <- exit
	}()
	select {
	case <-ready.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	cancel(SignalError{Signal: syscall.SIGTERM})
	select {
	case exit := <-done:
		if exit != 42 {
			t.Fatalf("exit=%d", exit)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("child did not exit")
	}
}
