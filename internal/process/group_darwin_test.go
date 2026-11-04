package process

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestDarwinOwnedGroupObservesUnreapedChild(t *testing.T) {
	if state, err := ownedChildState(os.Getpid()); err != nil || state != childRunning {
		t.Fatalf("query running self: state=%d error=%v", state, err)
	}
	command := exec.Command("/bin/sleep", "30")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	pid := command.Process.Pid
	if alive, err := ownedGroupAlive(pid); err != nil || !alive {
		t.Fatalf("query live group: alive=%v error=%v", alive, err)
	}
	for _, step := range []struct {
		signal syscall.Signal
		state  childState
	}{{syscall.SIGSTOP, childStopped}, {syscall.SIGKILL, childExited}} {
		if err := command.Process.Signal(step.signal); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			state, err := ownedChildState(pid)
			if err != nil {
				t.Fatalf("query unreaped child after %s: %v", step.signal, err)
			}
			if state == step.state {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("query child after %s: state=%d want=%d", step.signal, state, step.state)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if alive, err := ownedGroupAlive(pid); err != nil || alive {
		t.Fatalf("query group with only its unreaped leader: alive=%v error=%v", alive, err)
	}
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		if err := signalOwnedGroup(pid, signal); err != nil {
			t.Fatalf("signal verified empty group after child exit: %v", err)
		}
	}
	var exit *exec.ExitError
	if err := command.Wait(); !errors.As(err, &exit) {
		t.Fatalf("reap killed child: %v", err)
	}
	if _, err := ownedChildState(pid); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("query reaped child must report absence, not an I/O failure: %v", err)
	}
}
