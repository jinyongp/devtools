//go:build !windows

package process

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestOwnedGroupDescendantGraceAfterParentExit(t *testing.T) {
	for _, mode := range []string{"exit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			env := Environment(os.Environ(), map[string]string{"DEVTOOLS_TEST_OWNED_ROLE": mode, "DEVTOOLS_TEST_OWNED_DIR": dir})
			var stdout, stderr bytes.Buffer
			if mode == "cancel" {
				go func() {
					if waitOwnedFixture(ctx, filepath.Join(dir, "ready")) {
						cancel()
					}
				}()
			}
			code, condition := Execute(ctx, []string{os.Args[0], "-test.run=^TestOwnedGroupHelper$"}, dir, env, nil, &stdout, &stderr)
			if condition != nil || mode == "exit" && code != 23 || mode == "cancel" && code != 143 {
				t.Fatalf("code=%d error=%v stdout=%q stderr=%q", code, condition, stdout.String(), stderr.String())
			}
			if _, err := os.Stat(filepath.Join(dir, "cleaned")); err != nil {
				t.Fatalf("descendant cleanup hook was cut short: %v", err)
			}
			if stdout.String() != "child output\n" {
				t.Fatalf("inherited pipe was not drained: %q", stdout.String())
			}
		})
	}
}

func waitOwnedFixture(ctx context.Context, path string) bool {
	for ctx.Err() == nil {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestFailedStartObservationCleansOwnedDescendants(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_TEST_OWNED_ROLE": "cancel", "DEVTOOLS_TEST_OWNED_DIR": dir})
	condition := protocol.NewError("fixture_start_failed", "Cannot publish startup observation.", 3, nil)
	_, actual := ExecuteOwned(ctx, []string{os.Args[0], "-test.run=^TestOwnedGroupHelper$"}, dir, env, nil, &bytes.Buffer{}, &bytes.Buffer{}, func() *protocol.Error {
		if !waitOwnedFixture(ctx, filepath.Join(dir, "ready")) {
			return protocol.ContextError(ctx.Err())
		}
		return condition
	})
	if actual != condition {
		t.Fatalf("startup failure lost its original condition: %v", actual)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ready"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if state, err := ownedChildState(pid); err != nil {
		if !os.IsNotExist(err) && !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("cannot verify descendant termination: %v", err)
		}
	} else if state != childExited {
		t.Fatal("startup observation failure left an owned descendant alive")
	}
}

func TestOwnedGroupHelper(t *testing.T) {
	role := os.Getenv("DEVTOOLS_TEST_OWNED_ROLE")
	if role == "" {
		return
	}
	dir := os.Getenv("DEVTOOLS_TEST_OWNED_DIR")
	if role == "child" {
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, syscall.SIGTERM)
		if os.WriteFile(filepath.Join(dir, "ready"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(91)
		}
		<-interrupt
		time.Sleep(200 * time.Millisecond)
		if os.WriteFile(filepath.Join(dir, "cleaned"), nil, 0600) != nil {
			os.Exit(92)
		}
		_, _ = os.Stdout.WriteString("child output\n")
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestOwnedGroupHelper$")
	child.Env = Environment(os.Environ(), map[string]string{"DEVTOOLS_TEST_OWNED_ROLE": "child"})
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if child.Start() != nil {
		os.Exit(93)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !waitOwnedFixture(ctx, filepath.Join(dir, "ready")) {
		_ = child.Process.Kill()
		os.Exit(94)
	}
	if role == "exit" {
		os.Exit(23)
	}
	_ = child.Wait()
	os.Exit(0)
}
