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

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
)

type rewrapProtector struct {
	Protector
	backend     string
	removeError error
	removals    int
	failUnwrap  int
	unwraps     int
}

func (p *rewrapProtector) Identity() ProtectionIdentity {
	id := p.Protector.Identity()
	if p.backend != "" {
		id.Backend = p.backend
	}
	return id
}
func (p *rewrapProtector) Unwrap(ctx context.Context, b []byte) ([]byte, error) {
	p.unwraps++
	if p.unwraps == p.failUnwrap {
		return nil, ErrDenied
	}
	return p.Protector.Unwrap(ctx, b)
}
func (p *rewrapProtector) Remove(ctx context.Context) error { p.removals++; return p.removeError }

func TestRewrapKeepsRootAndOnlyRetiresOldAfterInstalledVerification(t *testing.T) {
	dir, old, want := sessionFixture(t)
	ctx := context.Background()
	store := KeyFileStore{Directory: dir}
	nextPass := NewMaterial([]byte("synthetic-next-passphrase"))
	defer nextPass.Destroy()
	next := PassphraseProtector{VaultID: old.VaultID, Passphrase: nextPass}
	id, _ := NewID()
	before, _ := store.Read()
	if _, err := store.Rewrap(ctx, id, old, next); err != nil {
		t.Fatal(err)
	}
	installed, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	got, err := unwrapRoot(ctx, next, installed)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Destroy()
	if !sameMaterial(want, got) || keyFileDigest(before) == keyFileDigest(installed) {
		t.Fatal("root changed or protection not replaced")
	}
	if _, err := old.Unwrap(ctx, installed.Wrapped); err == nil {
		t.Fatal("old passphrase opens new slot")
	}
	replay, err := store.Rewrap(ctx, id, old, next)
	if err != nil || !replay.Replayed {
		t.Fatal("rewrap replay", err)
	}
	if _, err := os.Stat(filepath.Join(dir, rewrapPending)); !os.IsNotExist(err) {
		t.Fatal("pending rewrap retained")
	}
	// Model interruption after the durable receipt and before pending cleanup.
	r, err := platformfs.OpenRoot(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := r.ReadFile(rewrapReceipt(id), MaxKeySlotBytes*8)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.WriteFile(ctx, rewrapPending, b, false); err != nil {
		t.Fatal(err)
	}
	replay, err = store.Rewrap(ctx, id, old, next)
	if err != nil || !replay.Replayed {
		t.Fatal("receipt cleanup recovery", err)
	}
}

func TestRewrapFailurePreservesRecoverableSlotAndManualLatch(t *testing.T) {
	dir, pass, want := sessionFixture(t)
	ctx := context.Background()
	store := KeyFileStore{Directory: dir}
	// A native backend stand-in receives only synthetic material. Removal is
	// observable; the real native adapters have their separate platform gates.
	original, _ := store.Read()
	original.Backend = "keychain"
	r, err := platformfs.OpenRoot(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.WriteFile(ctx, keyFilename, keyFileBytes(original), true); err != nil {
		t.Fatal(err)
	}
	old := &rewrapProtector{Protector: pass, backend: "keychain", removeError: ErrDenied}
	nextPass := NewMaterial([]byte("synthetic-new-protection"))
	defer nextPass.Destroy()
	next := &rewrapProtector{Protector: PassphraseProtector{VaultID: pass.VaultID, Passphrase: nextPass}, failUnwrap: 3}
	id, _ := NewID()
	// The new installed slot's actual unwrap fails. The old native entry must
	// remain and the durable encrypted pending state must permit explicit retry.
	if _, err = store.Rewrap(ctx, id, old, next); !errors.Is(err, ErrDenied) {
		t.Fatal("installed verification fixture", err)
	}
	if old.removals != 0 {
		t.Fatal("retired old slot before installed verification")
	}
	next.failUnwrap = 0
	if _, err = store.Rewrap(ctx, id, old, next); !errors.Is(err, ErrDenied) || old.removals != 1 {
		t.Fatal("retirement failure not recoverable", err)
	}
	// A concurrent operation cannot take over the pending goal.
	other, _ := NewID()
	if _, err = store.Rewrap(ctx, other, old, next); !errors.Is(err, ErrRecovery) {
		t.Fatal("pending rewrap bypassed", err)
	}
	old.removeError = nil
	result, err := store.Rewrap(ctx, id, old, next)
	if err != nil || !result.Replayed || old.removals != 2 {
		t.Fatal("pending retirement retry", err)
	}
	installed, _ := store.Read()
	actual, err := unwrapRoot(ctx, next, installed)
	if err != nil {
		t.Fatal(err)
	}
	defer actual.Destroy()
	if !sameMaterial(actual, want) {
		t.Fatal("recovery generated a new root")
	}
	latch, _ := json.Marshal(manualLock{Format: StorageVersion, VaultID: pass.VaultID, Locked: true})
	if err = maintenance.Write(filepath.Join(dir, "manual-lock.json"), latch); err != nil {
		t.Fatal(err)
	}
	nextNext := PassphraseProtector{VaultID: pass.VaultID, Passphrase: NewMaterial([]byte("synthetic-third-protection"))}
	defer nextNext.Passphrase.Destroy()
	another, _ := NewID()
	if _, err = store.Rewrap(ctx, another, next, nextNext); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "manual-lock.json"))
	if err != nil || string(got) != string(latch) {
		t.Fatal("rewrap reset manual lock", err)
	}
	session := NewKeySession(dir)
	defer session.Close()
	if session.Status().State != LockedManual {
		t.Fatal("manual lock did not survive rewrap and session restart")
	}

}

type rewrapFixtureInput struct {
	VaultID, ID, Stage string
	Old, Next          []byte
}

func runRewrapFixture() {
	var input rewrapFixtureInput
	if json.NewDecoder(io.LimitReader(os.Stdin, 1<<16)).Decode(&input) != nil {
		os.Exit(90)
	}
	old := NewMaterial(input.Old)
	next := NewMaterial(input.Next)
	clear(input.Old)
	clear(input.Next)
	defer old.Destroy()
	defer next.Destroy()
	_, err := (KeyFileStore{Directory: os.Args[2]}).rewrap(context.Background(), input.ID, PassphraseProtector{VaultID: input.VaultID, Passphrase: old}, PassphraseProtector{VaultID: input.VaultID, Passphrase: next}, func(stage string) {
		if stage == input.Stage {
			os.Exit(99)
		}
	})
	if err != nil {
		os.Exit(91)
	}
}
func TestRewrapRecoveryAtDurableBoundaries(t *testing.T) {
	for _, stage := range []string{"prepared", "slot-published", "installed-verified", "old-retired", "receipt-written", "cleaned"} {
		t.Run(stage, func(t *testing.T) {
			dir, old, master := sessionFixture(t)
			nextPass := NewMaterial([]byte("synthetic-rewrap-recovery-password"))
			defer nextPass.Destroy()
			next := PassphraseProtector{VaultID: old.VaultID, Passphrase: nextPass}
			id, _ := NewID()
			input := rewrapFixtureInput{VaultID: old.VaultID, ID: id, Stage: stage, Old: old.Passphrase.CopyBytes(), Next: nextPass.CopyBytes()}
			b, err := json.Marshal(input)
			clear(input.Old)
			clear(input.Next)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(b)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			childCtx, cancelChild := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancelChild()
			child := exec.CommandContext(childCtx, executable, "__vault-rewrap-fixture", dir)
			child.Stdin = bytes.NewReader(b)
			started := time.Now()
			err = child.Run()
			cancelChild()
			t.Logf("owned crash checkpoint elapsed: %s", time.Since(started))
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 99 {
				t.Fatal("owned rewrap process did not reach checkpoint", err)
			}
			info, infoErr := (KeyFileStore{Directory: dir}).RewrapInfo(id)
			if infoErr != nil || info == nil || info.RequestID == "" || info.FromBackend != "passphrase" || info.Backend != "passphrase" || info.IntegrityVerified {
				t.Fatal("missing passive recovery metadata", infoErr)
			}
			if info.SlotPublished != (stage != "prepared") || info.Completed != (stage == "cleaned") {
				t.Fatal("wrong recovery authentication route", stage, info)
			}
			// Crash delivery, explicit recovery and independent key verification
			// are separate operations; each retains the same bounded deadline.
			retryCtx, cancelRetry := context.WithTimeout(context.Background(), 20*time.Second)
			started = time.Now()
			result, err := (KeyFileStore{Directory: dir}).Rewrap(retryCtx, id, old, next)
			cancelRetry()
			t.Logf("explicit recovery elapsed: %s", time.Since(started))
			if err != nil || !result.Changed || !result.Replayed {
				t.Fatal("durable rewrap retry", err)
			}
			current, err := (KeyFileStore{Directory: dir}).Read()
			if err != nil {
				t.Fatal(err)
			}
			verifyCtx, cancelVerify := context.WithTimeout(context.Background(), 20*time.Second)
			started = time.Now()
			actual, err := unwrapRoot(verifyCtx, next, current)
			cancelVerify()
			t.Logf("independent key verification elapsed: %s", time.Since(started))
			if err != nil {
				t.Fatal(err)
			}
			defer actual.Destroy()
			if !sameMaterial(master, actual) {
				t.Fatal("recovery replaced the random root")
			}
			rootKey := master.CopyBytes()
			defer clear(rootKey)
			err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.Type().IsRegular() {
					return nil
				}
				contents, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if bytes.Contains(contents, rootKey) || bytes.Contains(contents, []byte("synthetic-rewrap-recovery-password")) {
					t.Fatal("plaintext root/passphrase in recovery state")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRewrapDrainsAdmissionAndInvalidatesFrozenTicket(t *testing.T) {
	dir, old, _ := sessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := (SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Retire(context.Background())
	if err = client.Unlock(ctx, old.Passphrase, false); err != nil {
		t.Fatal(err)
	}
	key, ticket, err := client.UseWithTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	launchID, _ := NewID()
	admission, err := client.BeginLaunch(ctx, launchID, *ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer admission.Finish(context.Background(), "canceled")
	nextPass := NewMaterial([]byte("synthetic-rewrap-ticket-target"))
	defer nextPass.Destroy()
	next := PassphraseProtector{VaultID: old.VaultID, Passphrase: nextPass}
	operation, _ := NewID()
	prepared := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := (KeyFileStore{Directory: dir}).rewrap(ctx, operation, old, next, func(stage string) {
			if stage == "prepared" {
				close(prepared)
			}
		})
		done <- e
	}()
	select {
	case <-prepared:
	case <-ctx.Done():
		t.Fatal("preparation blocked on admitted spawn")
	}
	before, err := (KeyFileStore{Directory: dir}).Read()
	if err != nil {
		t.Fatal(err)
	}
	oldRoot, err := unwrapRoot(ctx, old, before)
	if err != nil {
		t.Fatal("published across active admission", err)
	}
	oldRoot.Destroy()
	select {
	case e := <-done:
		t.Fatal("rewrap crossed admitted launch", e)
	default:
	}
	if err = admission.Finish(ctx, "spawned"); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = client.Unlock(ctx, nextPass, false); err != nil {
		t.Fatal("target explicit unlock after publication", err)
	}
	newID, _ := NewID()
	if a, e := client.BeginLaunch(ctx, newID, *ticket); !errors.Is(e, ErrLocked) {
		if a != nil {
			a.Finish(ctx, "canceled")
		}
		t.Fatal("pre-rewrap ticket survived explicit unlock", e)
	}
	key, fresh, err := client.UseWithTicket(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	if fresh.Epoch == ticket.Epoch {
		t.Fatal("slot publication did not change epoch")
	}
	newID, _ = NewID()
	a, err := client.BeginLaunch(ctx, newID, *fresh)
	if err != nil {
		t.Fatal("fresh ticket rejected", err)
	}
	if err = a.Finish(ctx, "canceled"); err != nil {
		t.Fatal(err)
	}
}

type existingNativeRewrap struct {
	root    *Material
	vaultID string
}

func (p existingNativeRewrap) Identity() ProtectionIdentity {
	return ProtectionIdentity{Backend: "keychain", VaultID: p.vaultID}
}
func (p existingNativeRewrap) Wrap(context.Context, []byte) ([]byte, error) { return nil, ErrRecovery }
func (p existingNativeRewrap) Unwrap(_ context.Context, reference []byte) ([]byte, error) {
	if string(reference) != "keychain/v1/"+p.vaultID {
		return nil, ErrInvalid
	}
	return p.root.CopyBytes(), nil
}
func TestRewrapAdoptsOnlyMatchingPreviouslyCreatedNativeItem(t *testing.T) {
	dir, old, master := sessionFixture(t)
	ctx := context.Background()
	id, _ := NewID()
	wrong, _ := NewKey()
	defer wrong.Destroy()
	if _, err := (KeyFileStore{Directory: dir}).Rewrap(ctx, id, old, existingNativeRewrap{root: wrong, vaultID: old.VaultID}); !errors.Is(err, ErrIntegrity) {
		t.Fatal("adopted unrelated native root", err)
	}
	before, err := (KeyFileStore{Directory: dir}).Read()
	if err != nil {
		t.Fatal(err)
	}
	if before.Backend != "passphrase" {
		t.Fatal("failed adoption replaced old protection")
	}
	result, err := (KeyFileStore{Directory: dir}).Rewrap(ctx, id, old, existingNativeRewrap{root: master, vaultID: old.VaultID})
	if err != nil || !result.Changed {
		t.Fatal("matching native preparation cannot recover", err)
	}
}

func TestNativeProtectionNoopHasDurableReplay(t *testing.T) {
	dir, old, master := sessionFixture(t)
	ctx := context.Background()
	native := existingNativeRewrap{root: master, vaultID: old.VaultID}
	first, _ := NewID()
	store := KeyFileStore{Directory: dir}
	if _, err := store.Rewrap(ctx, first, old, native); err != nil {
		t.Fatal(err)
	}
	id, _ := NewID()
	result, err := store.Rewrap(ctx, id, native, native)
	if err != nil || result.Changed || result.Replayed {
		t.Fatal("native no-op", err)
	}
	result, err = store.Rewrap(ctx, id, native, native)
	if err != nil || result.Changed || !result.Replayed {
		t.Fatal("native no-op has no replay receipt", err)
	}
}
