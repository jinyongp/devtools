//go:build linux || darwin

package project

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestResolveRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	dir := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(dir, Filename), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan *protocol.Error, 1)
	go func() {
		_, err := Resolve(dir, "")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || err.Code != "io_error" {
			t.Fatal("special configuration file was not rejected", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("project resolution waited for a FIFO writer")
	}
}
