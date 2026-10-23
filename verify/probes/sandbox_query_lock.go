//go:build linux || darwin

// Native syscall probe; the product has no test-only lock switches.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func emit(value any) { _ = json.NewEncoder(os.Stdout).Encode(value) }

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "expected operation and fixture path")
		os.Exit(2)
	}
	operation, path := os.Args[1], os.Args[2]
	if operation == "deny-writes" {
		checks := map[string]func() error{
			"read_write_open": func() error {
				f, err := os.OpenFile(filepath.Join(path, "lock"), os.O_CREATE|os.O_RDWR, 0600)
				if f != nil {
					_ = f.Close()
				}
				return err
			},
			"create": func() error { return os.WriteFile(filepath.Join(path, "new"), []byte("new"), 0600) },
			"write":  func() error { return os.WriteFile(filepath.Join(path, "write"), []byte("changed"), 0600) },
			"rename": func() error { return os.Rename(filepath.Join(path, "rename"), filepath.Join(path, "renamed")) },
			"unlink": func() error { return os.Remove(filepath.Join(path, "unlink")) },
		}
		passed := true
		for name, check := range checks {
			err := check()
			denied := errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS)
			emit(map[string]any{"check": name, "denied": denied})
			passed = passed && denied
		}
		if !passed {
			os.Exit(1)
		}
		return
	}
	flags, mode := os.O_RDONLY, syscall.LOCK_SH
	if operation == "hold-write" || operation == "try-write" {
		flags, mode = os.O_RDWR|os.O_CREATE, syscall.LOCK_EX
	}
	file, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		emit(map[string]any{"event": "open_failed", "denied": os.IsPermission(err)})
		os.Exit(1)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	for {
		err = syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			emit(map[string]any{"event": "lock_failed"})
			os.Exit(1)
		}
		if operation != "cancel-read" {
			emit(map[string]any{"event": "busy"})
			return
		}
		select {
		case <-ctx.Done():
			emit(map[string]any{"event": "deadline_exceeded"})
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	emit(map[string]any{"event": "acquired"})
	if operation == "hold-read" || operation == "hold-write" {
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
	}
}
