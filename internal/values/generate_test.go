package values

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func generatedToken(t *testing.T, files vault.FileStore, master *vault.Material, env string) string {
	t.Helper()
	p, key, err := files.ReadWithMaster(context.Background(), "app", master)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	values, _, err := p.Environment(env, key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return values["TOKEN"]
}

func TestSecretGenerateRecipeReplayAndPrivateStorage(t *testing.T) {
	ctx := context.Background()
	root, files, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	seed := []byte("0123456789abcdefghijklmnopqrstuv")
	reader := bytes.NewReader(seed)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls), generateReader: reader}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	c := Change{Action: "secret.generate", Key: "TOKEN", Revision: view.Revision, RequestID: testRequestID(t), Generate: &GenerateChange{}}
	first, e := store.Apply(ctx, c)
	if e != nil || !first.Changed || reader.Len() != 0 {
		t.Fatal("generation did not consume exactly 32 bytes", e)
	}
	token := generatedToken(t, files, master, "")
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(token) != 43 || len(decoded) != 32 || !bytes.Equal(decoded, seed) {
		t.Fatal("generated value violates fixed recipe", err)
	}
	clear(decoded)
	// No bytes remain in reader: replay would fail if it attempted generation.
	if replay, e := store.Apply(ctx, c); e != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatal("committed response loss regenerated", e)
	}
	raw, err := json.Marshal(first)
	if err != nil || bytes.Contains(raw, []byte(token)) {
		t.Fatal("generation result exposed plaintext", err)
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, []byte(token)) || bytes.Contains(body, seed) {
			t.Error("generated plaintext persisted outside encrypted storage")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, e := store.Update(ctx, func(editor *Editor) (bool, *protocol.Error) {
		return editor.Set(Secret, "TOKEN", "", "synthetic-later-secret")
	}); e != nil {
		t.Fatal(e)
	}
	if replay, e := store.Apply(ctx, c); e != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatal("later edit prevented replay", e)
	}
	if generatedToken(t, files, master, "") != "synthetic-later-secret" {
		t.Fatal("generation replay overwrote later writer")
	}
	if err := files.VerifyStorage(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSecretGenerateFaultLeavesNoProfileOrReceipt(t *testing.T) {
	ctx := context.Background()
	root, _, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls), generateReader: bytes.NewReader([]byte("too short"))}
	path := filepath.Join(store.Directory, profilekey.CanonicalFilename("app"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	c := Change{Action: "secret.generate", Key: "TOKEN", Revision: view.Revision, RequestID: testRequestID(t), Generate: &GenerateChange{}}
	if _, e := store.Apply(ctx, c); e == nil {
		t.Fatal("short randomness succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("RNG failure changed profile", err)
	}
	receipt := (Store{Directory: store.Directory, Profile: store.Profile}).receiptRelative(c.RequestID)
	if _, err := os.Stat(filepath.Join(root, receipt)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("RNG failure bound request ID")
	}
	store.generateReader = bytes.NewReader(bytes.Repeat([]byte{0x41}, 32))
	if _, e := store.Apply(ctx, c); e != nil {
		t.Fatal("precommit failure prevented retry", e)
	}
}

func TestSecretGenerateConcurrentCandidatesPublishOneValue(t *testing.T) {
	ctx := context.Background()
	root, files, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	c := Change{Action: "secret.generate", Key: "TOKEN", Revision: view.Revision, RequestID: testRequestID(t), Generate: &GenerateChange{}}
	paused, ready, resume := pauseCommit(t, store)
	paused.generateReader = bytes.NewReader(bytes.Repeat([]byte{0x41}, 32))
	type outcome struct {
		result ChangeResult
		err    *protocol.Error
	}
	done := make(chan outcome, 1)
	go func() { r, e := paused.Apply(ctx, c); done <- outcome{r, e} }()
	awaitPrepared(t, ready)
	winnerBytes := bytes.Repeat([]byte{0x42}, 32)
	store.generateReader = bytes.NewReader(winnerBytes)
	first, e := store.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	before, key, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	version := before.Values[0].Slots[0].Version
	key.Destroy()
	resume()
	last := <-done
	if last.err != nil || !last.result.Replayed || last.result.Revision != first.Revision {
		t.Fatal("losing candidate did not replay winner", last.err)
	}
	token := generatedToken(t, files, master, "")
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !bytes.Equal(decoded, winnerBytes) {
		t.Fatal("losing random candidate escaped", err)
	}
	after, key, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	if len(after.Values) != 1 || after.Values[0].Slots[0].Version != version {
		t.Fatal("same request published two value versions")
	}
}

func TestSecretGenerateLayerKindAndExpiry(t *testing.T) {
	ctx := context.Background()
	root, files, master, profileKey := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	if _, e := store.Update(ctx, func(editor *Editor) (bool, *protocol.Error) {
		if _, e := editor.Set(Variable, "PUBLIC", "", "value"); e != nil {
			return false, e
		}
		return editor.Set(Secret, "TOKEN", "", "")
	}); e != nil {
		t.Fatal(e)
	}
	apply := func(key, env string, replace bool) (ChangeResult, *protocol.Error) {
		t.Helper()
		view, e := store.Inspect(ctx, env)
		if e != nil {
			t.Fatal(e)
		}
		return store.Apply(ctx, Change{Action: "secret.generate", Key: key, Env: env, Revision: view.Revision, RequestID: testRequestID(t), Generate: &GenerateChange{Replace: replace}})
	}
	store.generateReader = bytes.NewReader(nil)
	for _, tc := range []struct {
		key     string
		replace bool
		code    string
	}{{"PUBLIC", false, "kind_conflict"}, {"PUBLIC", true, "kind_conflict"}, {"TOKEN", false, "value_exists"}} {
		if _, e := apply(tc.key, "", tc.replace); e == nil || e.Code != tc.code {
			t.Fatal("precondition consumed RNG or changed kind", e)
		}
	}
	store.generateReader = bytes.NewReader(bytes.Repeat([]byte{0x43}, 32))
	if _, e := apply("TOKEN", "local", false); e != nil {
		t.Fatal("inherited common blocked override", e)
	}
	expiry := time.Now().Add(time.Hour).Unix()
	setExpiry := func(expiry int64) {
		t.Helper()
		if _, err := files.Update(ctx, "app", profileKey, func(p *vault.Profile) (bool, error) {
			for i := range p.Values {
				if p.Values[i].Name == "TOKEN" {
					for j := range p.Values[i].Slots {
						p.Values[i].Slots[j].ExpiresAt = &expiry
					}
				}
			}
			p.Revision++
			return true, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	setExpiry(expiry)
	store.generateReader = bytes.NewReader(bytes.Repeat([]byte{0x44}, 32))
	if _, e := apply("TOKEN", "", true); e != nil {
		t.Fatal("live replacement", e)
	}
	p, key, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		t.Fatal(err)
	}
	key.Destroy()
	for _, value := range p.Values {
		if value.Name == "TOKEN" {
			for _, slot := range value.Slots {
				if slot.ExpiresAt == nil || *slot.ExpiresAt != expiry {
					t.Fatal("replacement removed expiry")
				}
			}
		}
	}
	setExpiry(0)
	store.generateReader = bytes.NewReader(nil)
	for _, env := range []string{"", "local"} {
		if _, e := apply("TOKEN", env, false); e == nil || e.Code != "value_exists" {
			t.Fatal("expired default creation", e)
		}
		if _, e := apply("TOKEN", env, true); e == nil || e.Code != "value_expired" {
			t.Fatal("expired replacement consumed RNG", e)
		}
	}
}

func TestSecretGenerateExpiryAtCommitRejectsWithoutReceipt(t *testing.T) {
	ctx := context.Background()
	root, files, master, profileKey := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls), generateReader: bytes.NewReader(bytes.Repeat([]byte{0x45}, 32))}
	if _, e := store.Update(ctx, func(editor *Editor) (bool, *protocol.Error) {
		return editor.Set(Secret, "TOKEN", "", "synthetic-live-value")
	}); e != nil {
		t.Fatal(e)
	}
	expiry := time.Now().Add(2 * time.Second).Unix()
	if _, err := files.Update(ctx, "app", profileKey, func(p *vault.Profile) (bool, error) {
		p.Values[0].Slots[0].ExpiresAt = &expiry
		p.Revision++
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	c := Change{Action: "secret.generate", Key: "TOKEN", Revision: view.Revision, RequestID: testRequestID(t), Generate: &GenerateChange{Replace: true}}
	path := filepath.Join(store.Directory, profilekey.CanonicalFilename("app"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	store.beforeCommit = func() { reached = true; time.Sleep(time.Until(time.Unix(expiry, 0))) }
	if _, e := store.Apply(ctx, c); e == nil || e.Code != "value_expired" || !reached {
		t.Fatal("expired during preparation was published", e)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("expired candidate changed stored value", err)
	}
	receipt := (Store{Directory: store.Directory, Profile: store.Profile}).receiptRelative(c.RequestID)
	if _, err := os.Stat(filepath.Join(root, receipt)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired candidate bound request")
	}
}
