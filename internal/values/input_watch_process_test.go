package values

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

type inputWatchFixture struct {
	Root      string
	Delivered *InputVersion
	DropHints bool
}

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "__managed-mutation-fixture" {
		os.Exit(runManagedMutationFixture())
	}
	if len(os.Args) == 3 && os.Args[1] == "__vault-session" {
		if vault.ServeKeySession(context.Background(), os.Args[2]) != nil {
			os.Exit(91)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "__input-watch-fixture" {
		var request inputWatchFixture
		if json.NewDecoder(io.LimitReader(os.Stdin, 65536)).Decode(&request) != nil || request.Delivered == nil {
			os.Exit(92)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		changes := WatchInputChanges(ctx, func(context.Context) (*vault.CommitHint, error) {
			if request.DropHints {
				return nil, nil
			}
			return vault.ReadCommitHint(request.Root)
		}, func(ctx context.Context) InputObservation {
			current, e := InspectInput(ctx, request.Root, "app", "local", request.Delivered.Spec)
			return ObserveInput(request.Delivered, current, e)
		})
		encoder := json.NewEncoder(os.Stdout)
		for change := range changes {
			if encoder.Encode(change) != nil {
				os.Exit(93)
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestInputWatchRealProcessRecoversLostHintsAndKeyDaemonReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	root, store, master, key := vaultSnapshotFixture(t, 1)
	var calls atomic.Int32
	snapshot, e := CaptureVault(ctx, root, "app", "local", true, time.Now(), snapshotMaster(master, &calls))
	if e != nil {
		t.Fatal(e)
	}
	delivered, err := NewInputVersion(snapshot.Metadata(), "local", InputSpec{Inject: true})
	snapshot.Destroy()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var daemons []*exec.Cmd
	t.Cleanup(func() {
		for _, cmd := range daemons {
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
		}
	})
	startDaemon := func() (*exec.Cmd, *vault.SessionClient) {
		t.Helper()
		cmd := exec.CommandContext(ctx, exe, "__vault-session", filepath.Join(root, "vault"))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		daemons = append(daemons, cmd)
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			client, err := vault.OpenSessionClient(filepath.Join(root, "vault"))
			if err == nil {
				if _, err = client.Status(ctx); err == nil {
					return cmd, client
				}
			}
			select {
			case <-deadline.C:
				t.Fatal("owned key daemon did not become ready")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	daemon, client := startDaemon()
	pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
	defer pass.Destroy()
	if err := client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	before, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(inputWatchFixture{Root: root, Delivered: delivered, DropHints: true})
	watcher := exec.CommandContext(ctx, exe, "__input-watch-fixture")
	watcher.Stdin = strings.NewReader(string(request))
	output, err := watcher.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if watcher.ProcessState == nil {
			watcher.Process.Kill()
			watcher.Wait()
		}
	})
	observations := make(chan InputObservation, 16)
	readErrors := make(chan error, 1)
	go func() {
		defer close(observations)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			line := scanner.Bytes()
			if strings.Contains(string(line), "synthetic-slot-value") {
				readErrors <- io.ErrUnexpectedEOF
				return
			}
			var observation InputObservation
			if err := json.Unmarshal(line, &observation); err != nil {
				readErrors <- err
				return
			}
			observations <- observation
		}
		if err := scanner.Err(); err != nil {
			readErrors <- err
		}
	}()
	wait := func(want string) InputObservation {
		t.Helper()
		timer := time.NewTimer(6 * time.Second)
		defer timer.Stop()
		for {
			select {
			case got, ok := <-observations:
				if !ok {
					t.Fatal("owned watcher closed early")
				}
				if got.State == want {
					return got
				}
			case err := <-readErrors:
				t.Fatal("invalid watcher metadata", err)
			case <-timer.C:
				t.Fatal("watcher failed to converge", want)
			}
		}
	}
	wait("current")
	if _, err := store.Update(ctx, "app", key, func(p *vault.Profile) (bool, error) {
		return p.Set("SECRET_0000", vault.Secret, "", "synthetic-new-observed-input", key)
	}); err != nil {
		t.Fatal(err)
	}
	pending := wait("pending")
	if same, known := SameInput(delivered, pending.Current); same || !known {
		t.Fatal("lost hint was not recovered from actual current storage")
	}
	// A real encrypted logical replacement changes incarnation while the hint
	// transport remains completely lost. Reconciliation must invalidate metadata.
	mutation, err := vault.AcquireLaunchMutation(ctx, filepath.Join(root, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		mutation()
		t.Fatal(err)
	}
	logical, err := store.LogicalSnapshotHeld(ctx, "app")
	var files map[string][]byte
	if err == nil {
		files, err = PrepareVaultRestoreHeld(ctx, root, store.VaultID, master, "app", logical)
	}
	clear(logical)
	if err == nil {
		id, _ := vault.NewID()
		_, err = (vault.ForwardStore{Root: root, VaultID: store.VaultID, Master: master}).ReplaceHeld(ctx, id, strings.Repeat("3", 64), files)
	}
	gate()
	mutation()
	if err != nil {
		t.Fatal(err)
	}
	replaced := wait("pending")
	if replaced.Current == nil || replaced.Current.Identity.StorageGeneration == pending.Current.Identity.StorageGeneration {
		t.Fatal("storage replacement retained an old current cache")
	}
	pending = replaced
	after, err := client.Status(ctx)
	if err != nil || before.IdleExpiresAt == nil || after.IdleExpiresAt == nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("periodic reconciliation extended key lifetime", err)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	locked := wait("verification_pending")
	if locked.Current != nil {
		t.Fatal("locked watcher retained a verified current goal")
	}
	if err := daemon.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	daemon.Wait()
	_, replacement := startDaemon()
	status, err := replacement.Status(ctx)
	if err != nil || status.State != vault.LockedManual {
		t.Fatal("replacement lost durable lock", err)
	}
	if err := replacement.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	restored := wait("pending")
	if same, known := SameInput(pending.Current, restored.Current); !same || !known {
		t.Fatal("daemon replacement lost latest goal")
	}
	// Restart the watcher itself with the original delivery. It must inspect
	// current storage on startup, without a persisted event cursor/cache clear.
	if err := watcher.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	watcher.Wait()
	restarted := exec.CommandContext(ctx, exe, "__input-watch-fixture")
	restarted.Stdin = strings.NewReader(string(request))
	restartedOut, err := restarted.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { restarted.Process.Kill(); restarted.Wait() }()
	scanner := bufio.NewScanner(restartedOut)
	if !scanner.Scan() {
		t.Fatal("restarted watcher produced no initial observation")
	}
	var initial InputObservation
	if json.Unmarshal(scanner.Bytes(), &initial) != nil || initial.State != "pending" {
		t.Fatal("restarted watcher failed to inspect current storage")
	}
}
