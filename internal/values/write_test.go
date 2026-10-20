package values

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func pauseCommit(t *testing.T, s ModernStore) (ModernStore, <-chan struct{}, func()) {
	t.Helper()
	ready, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	t.Cleanup(release)
	s.beforeCommit = func() { close(ready); <-resume }
	return s, ready, release
}
func awaitPrepared(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("candidate did not reach commit boundary")
	}
}
func setPublic(ctx context.Context, s ModernStore, value string) (bool, *protocol.Error) {
	return s.Update(ctx, func(e *Editor) (bool, *protocol.Error) { return e.Set(Variable, "URL", "", value) })
}
func TestPreparedWriteAllowsOtherProfileReadExecutionAndCommit(t *testing.T) {
	for _, protected := range []bool{false, true} {
		for _, managed := range []bool{false, true} {
			t.Run(fmt.Sprintf("protected=%v/managed=%v", protected, managed), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				root := privateTempDir(t)
				a := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
				var files vault.FileStore
				var master *vault.Material
				if protected {
					var calls atomic.Int32
					root, files, master, _ = vaultSnapshotFixture(t, 1)
					a.Directory = filepath.Join(root, "profiles")
					a.Master = snapshotMaster(master, &calls)
				}
				b := a
				b.Profile = "other"
				if _, e := setPublic(ctx, a, "before-a"); e != nil {
					t.Fatal(e)
				}
				if protected {
					if _, e := b.Update(ctx, func(e *Editor) (bool, *protocol.Error) { return e.Set(Secret, "TOKEN", "", "synthetic-other-profile") }); e != nil {
						t.Fatal(e)
					}
				}
				if _, e := setPublic(ctx, b, "before-b"); e != nil {
					t.Fatal(e)
				}
				paused, ready, resume := pauseCommit(t, a)
				done := make(chan *protocol.Error, 1)
				c := Change{}
				if managed {
					view, e := a.Inspect(ctx, "")
					if e != nil {
						t.Fatal(e)
					}
					text := "after-a"
					c = Change{Action: "variable.set", Key: "URL", Value: &text, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000411"}
				}
				go func() {
					if managed {
						_, e := paused.Apply(ctx, c)
						done <- e
					} else {
						_, e := setPublic(ctx, paused, "after-a")
						done <- e
					}
				}()
				awaitPrepared(t, ready)
				// These must finish while A remains paused, including a real execution input.
				short, stop := context.WithTimeout(ctx, 2*time.Second)
				defer stop()
				if _, e := b.Inspect(short, ""); e != nil {
					t.Fatal("B inspection blocked", e)
				}
				var snapshot *Snapshot
				var e *protocol.Error
				if protected {
					snapshot, e = CaptureVault(short, root, "other", "", true, time.Now(), b.Master)
				} else {
					snapshot, e = CapturePublic(short, root, "other", "", true, time.Now())
				}
				if e != nil {
					t.Fatal("B execution snapshot blocked", e)
				}
				snapshot.Destroy()
				if _, e := setPublic(short, b, "after-b"); e != nil {
					t.Fatal("B commit blocked", e)
				}
				resume()
				if e := <-done; e != nil {
					t.Fatal(e)
				}
				for _, s := range []ModernStore{a, b} {
					state, e := s.Read(ctx)
					if e != nil {
						t.Fatal(e)
					}
					actual, _, e := state.GetVariable("URL", "")
					want := "after-a"
					if s.Profile == "other" {
						want = "after-b"
					}
					if e != nil || actual != want {
						t.Fatalf("lost %s write: %s %v", s.Profile, actual, e)
					}
				}
				if protected {
					if err := files.VerifyStorage(ctx); err != nil {
						t.Fatal("catalog/profile authentication lost", err)
					}
				}
			})
		}
	}
}
func TestPreparedWriteRejectsSameProfileChangesIncludingNoop(t *testing.T) {
	for _, noop := range []bool{false, true} {
		t.Run(fmt.Sprint(noop), func(t *testing.T) {
			ctx := context.Background()
			s := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "app"}
			if _, e := setPublic(ctx, s, "before"); e != nil {
				t.Fatal(e)
			}
			paused, ready, resume := pauseCommit(t, s)
			done := make(chan *protocol.Error, 1)
			value := "candidate"
			if noop {
				value = "before"
			}
			go func() { _, e := setPublic(ctx, paused, value); done <- e }()
			awaitPrepared(t, ready)
			if _, e := setPublic(ctx, s, "winner"); e != nil {
				t.Fatal(e)
			}
			resume()
			if e := <-done; e == nil || e.Code != "revision_conflict" {
				t.Fatal("stale candidate accepted", e)
			}
			state, e := s.Read(ctx)
			if e != nil {
				t.Fatal(e)
			}
			got, _, e := state.GetVariable("URL", "")
			if e != nil || got != "winner" {
				t.Fatal("winner overwritten", e)
			}
		})
	}
}
func TestConcurrentManagedRequestReplaysAtCommit(t *testing.T) {
	ctx := context.Background()
	s := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "app"}
	view, e := s.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "same-request"
	c := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000412"}
	paused, ready, resume := pauseCommit(t, s)
	done := make(chan ChangeResult, 1)
	errs := make(chan *protocol.Error, 1)
	go func() { r, e := paused.Apply(ctx, c); done <- r; errs <- e }()
	awaitPrepared(t, ready)
	first, e := s.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	resume()
	last := <-done
	if e := <-errs; e != nil || !last.Replayed || last.Revision != first.Revision || last.Changed != first.Changed {
		t.Fatal("concurrent request not replayed", last, e)
	}
}
func TestPreparedWriteRejectsRotationAndRewrap(t *testing.T) {
	for _, rewrap := range []bool{false, true} {
		t.Run(fmt.Sprint(rewrap), func(t *testing.T) {
			ctx := context.Background()
			root, files, master, _ := vaultSnapshotFixture(t, 1)
			var calls atomic.Int32
			s := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
			paused, ready, resume := pauseCommit(t, s)
			done := make(chan *protocol.Error, 1)
			go func() { _, e := setPublic(ctx, paused, "stale"); done <- e }()
			awaitPrepared(t, ready)
			if rewrap {
				slot, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Read()
				if err != nil {
					t.Fatal(err)
				}
				old, next := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only")), vault.NewMaterial([]byte("synthetic-rewrapped-write"))
				defer old.Destroy()
				defer next.Destroy()
				id, _ := vault.NewID()
				if _, err = (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Rewrap(ctx, id, vault.PassphraseProtector{VaultID: slot.VaultID, Passphrase: old}, vault.PassphraseProtector{VaultID: slot.VaultID, Passphrase: next}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := files.RotateProfile(ctx, "app", master); err != nil {
					t.Fatal(err)
				}
			}
			resume()
			e := <-done
			if e == nil {
				t.Fatal("stale protection accepted")
			}
			want := "revision_conflict"
			if rewrap {
				want = vault.ErrInputChanged.Error()
			}
			if e.Code != want {
				t.Fatal(e)
			}
		})
	}
}
func TestPreparedWriteRejectsLockAndRelockEpoch(t *testing.T) {
	for _, unlockAgain := range []bool{false, true} {
		t.Run(fmt.Sprint(unlockAgain), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			root, _, _, _ := vaultSnapshotFixture(t, 1)
			dir := filepath.Join(root, "vault")
			client, err := (vault.SessionManager{Directory: dir}).Client(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if err := client.Retire(cleanup); err != nil {
					t.Error(err)
				}
			})
			pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
			defer pass.Destroy()
			if err = client.Unlock(ctx, pass, false); err != nil {
				t.Fatal(err)
			}
			s := CurrentStore(filepath.Join(root, "profiles"), "app")
			paused, ready, resume := pauseCommit(t, s)
			done := make(chan *protocol.Error, 1)
			go func() { _, e := setPublic(ctx, paused, "stale-authority"); done <- e }()
			awaitPrepared(t, ready)
			if err = client.Lock(ctx); err != nil {
				t.Fatal("lock blocked by candidate preparation", err)
			}
			if unlockAgain {
				if err = client.Unlock(ctx, pass, false); err != nil {
					t.Fatal(err)
				}
			}
			resume()
			e := <-done
			if e == nil {
				t.Fatal("old authorization reused")
			}
			want := vault.ErrLocked.Error()
			if unlockAgain {
				want = vault.ErrInputChanged.Error()
			}
			if e.Code != want {
				t.Fatal(e)
			}
		})
	}
}
func TestPreparedWriteRejectsCancellationAndPendingRecovery(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprint(pending), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := privateTempDir(t)
			s := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
			paused, ready, resume := pauseCommit(t, s)
			done := make(chan *protocol.Error, 1)
			go func() { _, e := setPublic(ctx, paused, "uncommitted"); done <- e }()
			awaitPrepared(t, ready)
			if pending {
				if err := maintenance.Write(filepath.Join(root, ".maintenance", "restore-pending.json"), []byte(`{"unsupported_pending":true}`)); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			resume()
			e := <-done
			if e == nil {
				t.Fatal("cancelled/recovery candidate published")
			}
			if !pending && e.Code != "canceled" {
				t.Fatal(e)
			}
		})
	}
}

func TestWriteSessionValidationPrecedesDiskLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	root, _, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	s := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	validated := false
	s.WriteMaster = func(ctx context.Context) (*vault.Material, func(context.Context) error, error) {
		key, err := s.Master(ctx)
		validate := func(ctx context.Context) error {
			// Native access must not occur while either of these disk leases is held.
			release, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(root, "vault", "key-slot-write.lock"))
			if err != nil {
				return err
			}
			if !free {
				return fmt.Errorf("validation ran inside key-slot lock")
			}
			defer release()
			gate, err := maintenance.AcquireExclusive(ctx, root)
			if err != nil {
				return err
			}
			gate()
			validated = true
			return nil
		}
		return key, validate, err
	}
	if _, e := setPublic(ctx, s, "valid"); e != nil {
		t.Fatal(e)
	}
	if !validated {
		t.Fatal("commit skipped session validation")
	}
}
