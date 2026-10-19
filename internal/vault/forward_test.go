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
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
)

const forwardCanary = "synthetic-forward-transition-secret"

type forwardFixtureRequest struct {
	VaultID, ID, Fingerprint, CrashAt string
	Master                            []byte
	After                             map[string][]byte
}

func runForwardFixture() {
	var request forwardFixtureRequest
	if json.NewDecoder(io.LimitReader(os.Stdin, 1<<20)).Decode(&request) != nil {
		os.Exit(90)
	}
	master := NewMaterial(request.Master)
	clear(request.Master)
	defer master.Destroy()
	store := ForwardStore{Root: os.Args[2], VaultID: request.VaultID, Master: master, checkpoint: func(stage string) {
		if stage == request.CrashAt {
			os.Exit(99)
		}
	}}
	if _, err := store.Replace(context.Background(), request.ID, request.Fingerprint, request.After); err != nil {
		os.Exit(91)
	}
}
func forwardFixture(t *testing.T) (ForwardStore, map[string][]byte, map[string][]byte, *Material) {
	t.Helper()
	base, err := os.MkdirTemp("", "dvf-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "data")
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	root.MkdirAll("profiles")
	root.MkdirAll(".maintenance")
	root.MkdirAll("vault")
	master, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(master.Destroy)
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(key.Destroy)
	vaultID, _ := NewID()
	p, err := NewSealedProfile("app", vaultID, master, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Set("TOKEN", Secret, "", forwardCanary, key); err != nil {
		t.Fatal(err)
	}
	encoded, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	catalog := Catalog{Format: StorageVersion, VaultID: vaultID, Entries: []CatalogEntry{entryFor(p)}}
	if err = catalog.Sign(master); err != nil {
		t.Fatal(err)
	}
	catalogBytes, _ := json.Marshal(catalog)
	profilePath := "profiles/" + profilekey.CanonicalFilename("app")
	before := map[string][]byte{profilePath: []byte(`{"version":1,"profile":"app","envs":{},"keys":{"TOKEN":{"kind":"secret","common":"` + forwardCanary + `","envs":{}}}}`), ".maintenance/values-key": bytes.Repeat([]byte{'L'}, 32)}
	for name, b := range before {
		if err = root.WriteFile(context.Background(), name, b, false); err != nil {
			t.Fatal(err)
		}
	}
	legacy := NewMaterial(before[".maintenance/values-key"])
	defer legacy.Destroy()
	protected, err := SealProtected(context.Background(), master, vaultID, vaultID, MaterialLegacyVerification, legacy)
	if err != nil {
		t.Fatal(err)
	}
	after := map[string][]byte{profilePath: encoded, catalogRelative: catalogBytes, ".maintenance/values-key": []byte(`{"vault_material":1}`), "vault/legacy-verification.json": protected}
	return ForwardStore{Root: path, VaultID: vaultID, Master: master}, before, after, key
}
func scanForwardCanaries(t *testing.T, root string, skip map[string][]byte) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() || strings.HasSuffix(path, ".lock") || strings.HasSuffix(path, ".turnstile") {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		if _, ok := skip[filepath.ToSlash(relative)]; ok {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(forwardCanary)) || bytes.Contains(b, bytes.Repeat([]byte{'L'}, 32)) {
			return errors.New("synthetic plaintext reached recovery/runtime scratch")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestEncryptedForwardRecoveryAtEveryDurableBoundary(t *testing.T) {
	profilePath := "profiles/" + profilekey.CanonicalFilename("app")
	stages := []string{"recovery-written", "intent-published", "file:.maintenance/values-key", "file:" + profilePath, "file:" + catalogRelative, "file:vault/legacy-verification.json", "receipt-written", "committed", "cleaned"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			store, before, after, key := forwardFixture(t)
			id, _ := NewID()
			fingerprint := strings.Repeat("1", 64)
			request := forwardFixtureRequest{VaultID: store.VaultID, ID: id, Fingerprint: fingerprint, CrashAt: stage, Master: store.Master.CopyBytes(), After: after}
			b, err := json.Marshal(request)
			clear(request.Master)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(b)
			exe, _ := os.Executable()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, exe, "__vault-forward-fixture", store.Root)
			child.Stdin = bytes.NewReader(b)
			err = child.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 99 {
				t.Fatal("durable checkpoint did not terminate owned process", err)
			}
			if stage == "recovery-written" {
				for relative, want := range before {
					got, err := os.ReadFile(filepath.Join(store.Root, relative))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatal("pre-intent failure changed original", err)
					}
				}
				scanForwardCanaries(t, store.Root, before)
				result, err := store.Replace(ctx, id, fingerprint, map[string][]byte{profilePath: []byte("must-not-replace-the-frozen-goal")})
				if err != nil || !result.Replayed || !result.Committed {
					t.Fatal("frozen preparation retry", err)
				}
			} else if stage == "cleaned" {
				result, err := store.Replace(ctx, id, fingerprint, after)
				if err != nil || !result.Replayed || !result.Committed {
					t.Fatal("lost completion response replay", err)
				}
			} else {
				if release, err := maintenance.AcquireShared(ctx, store.Root); err == nil {
					release()
					t.Fatal("ordinary/legacy maintenance accepted encrypted pending state")
				}
				wrong, _ := NewKey()
				defer wrong.Destroy()
				invalid := store
				invalid.Master = wrong
				if _, err := invalid.Recover(ctx); err == nil {
					t.Fatal("recovered with a missing/wrong protection key")
				}
				result, err := store.Recover(ctx)
				if err != nil || !result.Replayed || !result.Committed {
					t.Fatal("encrypted roll-forward recovery", err)
				}
			}
			for relative, want := range after {
				got, err := os.ReadFile(filepath.Join(store.Root, relative))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("mixed after-images after recovery", relative, err)
				}
			}
			p, err := (FileStore{Root: store.Root, VaultID: store.VaultID, CatalogKey: store.Master}).Read(ctx, "app", key)
			if err != nil {
				t.Fatal("recovered authenticated profile unreadable", err)
			}
			values, _, err := p.Environment("", key, time.Now())
			if err != nil || values["TOKEN"] != forwardCanary {
				t.Fatal("recovered value mismatch", err)
			}
			scanForwardCanaries(t, store.Root, nil)
			if _, err := store.Replace(ctx, id, strings.Repeat("2", 64), after); err == nil || err.Error() != "request_conflict" {
				t.Fatal("same UUID accepted changed logical input", err)
			}
		})
	}
}

func TestForwardPreparedArchiveRejectsStaleOriginal(t *testing.T) {
	store, _, after, _ := forwardFixture(t)
	id, _ := NewID()
	store.checkpoint = func(stage string) {
		if stage == "recovery-written" {
			panic("pause")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("checkpoint not reached")
			}
		}()
		store.Replace(context.Background(), id, strings.Repeat("1", 64), after)
	}()
	store.checkpoint = nil
	path := filepath.Join(store.Root, "profiles", profilekey.CanonicalFilename("app"))
	if err := maintenance.Write(path, []byte("synthetic concurrent modification")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Replace(context.Background(), id, strings.Repeat("1", 64), after); err == nil || err.Error() != "revision_conflict" {
		t.Fatal("stale preparation overwrote concurrent data", err)
	}
}

func TestCommittedRecoveryNeverRollsBackLaterMutation(t *testing.T) {
	store, _, after, _ := forwardFixture(t)
	id, _ := NewID()
	fingerprint := strings.Repeat("1", 64)
	if _, err := store.Replace(context.Background(), id, fingerprint, after); err != nil {
		t.Fatal(err)
	}
	// Model a committed pointer reappearing after interrupted directory cleanup.
	// Its durable commit receipt is valid, but later data must remain authoritative.
	path := filepath.Join(store.Root, "profiles", profilekey.CanonicalFilename("app"))
	later := []byte("synthetic later mutation")
	if err := maintenance.Write(path, later); err != nil {
		t.Fatal(err)
	}
	pointer, _ := json.Marshal(forwardPointer{Version: forwardVersion, ID: id, Phase: "committed", VaultID: store.VaultID})
	if err := maintenance.Write(filepath.Join(store.Root, forwardPending), pointer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, later) {
		t.Fatal("committed recovery reverted later state", err)
	}
}
