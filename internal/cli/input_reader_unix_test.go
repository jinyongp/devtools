//go:build !windows

package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInheritedBlockingPipeCancellation(t *testing.T) {
	fds := make([]int, 2)
	if err := unix.Pipe(fds); err != nil {
		t.Fatal(err)
	}
	reader, writer := os.NewFile(uintptr(fds[0]), "inherited-stdin"), os.NewFile(uintptr(fds[1]), "parent-writer")
	defer reader.Close()
	defer writer.Close()
	flags, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		body, e := readBoundedInput(ctx, reader, 64, "secret")
		if body != nil || e == nil || e.ExitCode != 130 {
			t.Errorf("cancellation: %v", e)
		}
	}()
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("inherited pipe worker did not stop")
	}
	after, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFL, 0)
	if err != nil || after != flags {
		t.Fatal("original input flags changed", after, flags, err)
	}
}
