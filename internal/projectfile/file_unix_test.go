//go:build linux || darwin

package projectfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestAttributeFailureIdentifiesNativeOperation(t *testing.T) {
	path, _ := fileFixture(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = attributes(f)
	if err == nil {
		t.Fatal("closed descriptor did not fail")
	}
	failure := ioFailure("read.metadata", err)
	facts := failure.Details["facts"].(map[string]any)
	if facts["stage"] != "read.metadata.flistxattr.size" || facts["os_code"] != uint64(unix.EBADF) {
		t.Fatal("attribute failure lost its OS diagnosis", failure.Details)
	}
}

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
