package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func launchFixture(t *testing.T) (*SessionClient, LaunchTicket) {
	t.Helper()
	dir, p, _ := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := (SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		// Only isolated synthetic fixtures can have their deliberately unknown
		// records removed without a product recovery proof.
		os.RemoveAll(filepath.Join(c.Directory, "launches"))
		c.Retire(ctx)
	})
	if err = c.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	key, ticket, err := c.UseWithTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	return c, *ticket
}

func TestLaunchLockBeforeAdmissionRejectsFrozenTicket(t *testing.T) {
	c, ticket := launchFixture(t)
	if err := c.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	id, _ := NewID()
	if a, err := c.BeginLaunch(context.Background(), id, ticket); !errors.Is(err, ErrLocked) {
		if a != nil {
			a.Finish(context.Background(), "canceled")
		}
		t.Fatal(err)
	}
}

func TestLaunchLockDrainsKnownOutcomeWithoutBlockingStatus(t *testing.T) {
	c, ticket := launchFixture(t)
	id, _ := NewID()
	a, err := c.BeginLaunch(context.Background(), id, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if a.release != nil {
			a.Finish(context.Background(), "canceled")
		}
	}()
	done := make(chan error, 1)
	go func() { done <- c.Lock(context.Background()) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err := c.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if status.State == LockedManual {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock intent not observable")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatal("ack before spawn outcome", err)
	default:
	}
	if err = a.Finish(context.Background(), "spawned"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lock did not drain")
	}
	if entries, err := os.ReadDir(filepath.Join(c.Directory, "launches")); err != nil || len(entries) != 0 {
		t.Fatal("terminal admission not retired", err)
	}
}

func TestLaunchUnknownSurvivesLiveLeaseLossAndSessionRestart(t *testing.T) {
	c, ticket := launchFixture(t)
	id, _ := NewID()
	a, err := c.BeginLaunch(context.Background(), id, ticket)
	if err != nil {
		t.Fatal(err)
	}
	// Finish with no confirmed outcome models loss of the launcher's OS lease:
	// the persistent unknown must survive even though the lock gate is now free.
	if err = a.Finish(context.Background(), "unknown"); !errors.Is(err, ErrLockPending) {
		t.Fatal(err)
	}
	if err = c.Lock(context.Background()); !errors.Is(err, ErrLockPending) {
		t.Fatal("unknown treated as canceled", err)
	}
	fresh := NewKeySession(c.Directory)
	defer fresh.Close()
	if err = fresh.Lock(context.Background()); !errors.Is(err, ErrLockPending) {
		t.Fatal("restart forgot unknown", err)
	}
	if status := fresh.Status(); status.State != LockedManual {
		t.Fatal(status.State)
	}
}

func TestLaunchAdmissionCannotBeReplayedOrRefreshed(t *testing.T) {
	c, ticket := launchFixture(t)
	expired := ticket
	expired.ExpiresAt = time.Now().Add(-time.Second)
	id, _ := NewID()
	if _, err := c.BeginLaunch(context.Background(), id, expired); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	a, err := c.BeginLaunch(context.Background(), id, ticket)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Finish(context.Background(), "canceled"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.BeginLaunch(context.Background(), id, ticket); !errors.Is(err, ErrRecovery) {
		t.Fatal("same operation admitted twice", err)
	}
	stale := ticket
	stale.SessionID, _ = NewID()
	if _, err = c.BeginLaunch(context.Background(), id, stale); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

// This helper is a real process owning its own shared OS launch gate. Its
// synthetic ticket is sent on an anonymous pipe, never a command argument.
func runLaunchFixture() {
	var ticket LaunchTicket
	if json.NewDecoder(io.LimitReader(os.Stdin, 4096)).Decode(&ticket) != nil {
		os.Exit(90)
	}
	client, err := OpenSessionClient(os.Args[2])
	if err != nil {
		os.Exit(91)
	}
	admission, err := client.BeginLaunch(context.Background(), os.Args[3], ticket)
	if err != nil {
		os.Exit(92)
	}
	if _, err = os.Stdout.Write([]byte{1}); err != nil {
		os.Exit(93)
	}
	var finish [1]byte
	if _, err = io.ReadFull(os.Stdin, finish[:]); err != nil {
		admission.Finish(context.Background(), "canceled")
		return
	}
	if admission.Finish(context.Background(), "canceled") != nil {
		os.Exit(94)
	}
}

func startLaunchFixture(t *testing.T, c *SessionClient, ticket LaunchTicket) (*exec.Cmd, io.WriteCloser, string) {
	t.Helper()
	exe, _ := os.Executable()
	id, _ := NewID()
	cmd := exec.Command(exe, "__vault-launch-fixture", c.Directory, id)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.Close()
		out.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	if err = json.NewEncoder(in).Encode(ticket); err != nil {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := io.ReadFull(out, b[:])
		if err == nil && b[0] != 1 {
			err = ErrInvalid
		}
		ready <- err
	}()
	select {
	case err = <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("launcher helper not admitted")
	}
	return cmd, in, id
}

func TestLaunchRealProcessCrashRetainsUnknown(t *testing.T) {
	c, ticket := launchFixture(t)
	child, _, id := startLaunchFixture(t, c, ticket)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := c.Lock(ctx)
	cancel()
	if err == nil {
		t.Fatal("lock acknowledged live admitted launcher")
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	if err = c.Lock(context.Background()); !errors.Is(err, ErrLockPending) {
		t.Fatal("dead launcher guessed canceled", err)
	}
	b, err := os.ReadFile(filepath.Join(c.Directory, launchName(id)))
	if err != nil || !bytes.Contains(b, []byte(`"outcome":"unknown"`)) {
		t.Fatal("crash lost unknown journal", err)
	}
}

func TestLaunchRealDaemonCrashDuringLockDrainsAcrossReplacement(t *testing.T) {
	dir, p, _ := sessionFixture(t)
	exe, _ := os.Executable()
	daemon := exec.Command(exe, "__vault-session", dir)
	daemon.Env = sessionDaemonEnvironment(os.Environ())
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if daemon.ProcessState == nil {
			daemon.Process.Kill()
			daemon.Wait()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var c *SessionClient
	for {
		var err error
		c, err = OpenSessionClient(dir)
		if err == nil {
			if _, err = c.Status(ctx); err == nil {
				break
			}
		}
		if err = waitSession(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Unlock(ctx, p.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	key, ticket, err := c.UseWithTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	child, in, _ := startLaunchFixture(t, c, *ticket)
	first := make(chan error, 1)
	go func() { first <- c.Lock(ctx) }()
	for {
		status, err := c.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == LockedManual {
			break
		}
		if err = waitSession(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = daemon.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	daemon.Wait()
	select {
	case err = <-first:
		if err == nil {
			t.Fatal("crashed coordinator returned lock success")
		}
	case <-time.After(time.Second):
		t.Fatal("dead daemon response did not close")
	}
	next, err := (SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { next.Retire(context.Background()) })
	status, err := next.Status(ctx)
	if err != nil || status.State != LockedManual {
		t.Fatal("replacement forgot durable intent", err)
	}
	done := make(chan error, 1)
	go func() { done <- next.Lock(ctx) }()
	select {
	case err := <-done:
		t.Fatal("replacement forgot launch lease", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err = in.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not drain known outcome")
	}
}
