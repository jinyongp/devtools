// vault-fixture retires only explicitly selected test sessions. It never obtains
// key material, initializes a session, or terminates a process from stored PIDs.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

func retire(ctx context.Context, directory string) error {
	if !filepath.IsAbs(directory) {
		return errors.New("test vault directory must be absolute")
	}
	if _, err := os.Lstat(filepath.Join(directory, "session.json")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return errors.New("cannot inspect test session")
	}
	client, err := vault.OpenSessionClient(directory)
	if err != nil {
		return errors.New("cannot verify test session identity")
	}
	if err = client.Retire(ctx); err != nil && !errors.Is(err, vault.ErrUnavailable) {
		return errors.New("test session retirement failed")
	}
	// The reply precedes worker shutdown. The actual lease proves that the
	// session has released its private directory pins before fixture deletion.
	for {
		release, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(directory, "session-daemon.lock"))
		if err != nil {
			return errors.New("cannot verify test session shutdown")
		}
		if free {
			release()
			return nil
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("test session shutdown timed out")
		case <-timer.C:
		}
	}
}
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "provide explicit owned test vault directories")
		os.Exit(2)
	}
	for _, directory := range os.Args[1:] {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := retire(ctx, directory)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
