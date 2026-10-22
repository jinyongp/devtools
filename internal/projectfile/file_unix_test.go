//go:build linux || darwin

package projectfile

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestReadRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devtools.toml")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan *protocol.Error, 1)
	go func() {
		_, err := Read(path)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || err.Code != "config_unsafe_target" {
			t.Fatal("special configuration file was not rejected", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("configuration read waited for a FIFO writer")
	}
}
