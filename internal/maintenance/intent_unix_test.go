//go:build !windows

package maintenance

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func waitForWriterIntent(t *testing.T, path string) {
	t.Helper()
	turnstile, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer turnstile.Close()
	deadline := time.Now().Add(time.Second)
	for {
		err = syscall.Flock(int(turnstile.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		_ = syscall.Flock(int(turnstile.Fd()), syscall.LOCK_UN)
		if time.Now().After(deadline) {
			t.Fatal("writer never acquired turnstile")
		}
		time.Sleep(5 * time.Millisecond)
	}

}
