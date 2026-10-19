package vault

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jinyongp/devtools/internal/profilekey"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func rotationFixture(t *testing.T) (*SessionClient, *Material, FileStore, *Profile, *Material, LaunchTicket) {
	t.Helper()
	client, ticket := launchFixture(t)
	master, err := client.Use(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(master.Destroy)
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(key.Destroy)
	p, err := NewSealedProfile("app", ticket.VaultID, master, key)
	if err != nil {
		t.Fatal(err)
	}
	p.CreateEnv("local", key)
	p.Set("TOKEN", Secret, "", "rotation-synthetic-common", key)
	p.Set("TOKEN", Secret, "local", "rotation-synthetic-override", key)
	p.Set("URL", Public, "", "https://example.invalid", key)
	expires := time.Now().Add(time.Hour).Unix()
	p.Values[0].Slots[0].ExpiresAt = &expires
	p.Sign(key)
	store := FileStore{Root: filepath.Dir(client.Directory), VaultID: ticket.VaultID, CatalogKey: master}
	if err = store.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = store.Create(context.Background(), p, key); err != nil {
		t.Fatal(err)
	}
	ticket.Profile = p.Name
	ticket.ProfileID = p.ID
	ticket.StorageGeneration = p.Generation
	ticket.KeyEpoch = p.KeyEpoch
	return client, master, store, p, key, ticket
}

func TestProfileRotationPreservesLogicalInputRejectsOldAdmission(t *testing.T) {
	client, master, store, p, oldKey, ticket := rotationFixture(t)
	ctx := context.Background()
	before, refs, err := p.Environment("local", oldKey, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	q, err := store.RotateProfile(ctx, p.Name, master)
	if err != nil {
		t.Fatal(err)
	}
	if q.KeyEpoch != p.KeyEpoch+1 || q.ID != p.ID || q.Generation != p.Generation || q.Revision != p.Revision || !reflect.DeepEqual(q.Envs, p.Envs) || !reflect.DeepEqual(q.Bundles, p.Bundles) {
		t.Fatal("logical identity changed during key rotation")
	}
	if err = q.Verify(oldKey); !errors.Is(err, ErrIntegrity) {
		t.Fatal("old encryption key still authenticates profile", err)
	}
	_, newKey, err := store.ReadWithMaster(ctx, p.Name, master)
	if err != nil {
		t.Fatal(err)
	}
	defer newKey.Destroy()
	after, newRefs, err := q.Environment("local", newKey, time.Now())
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(refs, newRefs) {
		t.Fatal("rotation changed logical values/versions", err)
	}
	if !reflect.DeepEqual(p.Values[0].Slots[0].ExpiresAt, q.Values[0].Slots[0].ExpiresAt) {
		t.Fatal("rotation changed expiry")
	}
	id, _ := NewID()
	admission, err := client.BeginLaunch(ctx, id, ticket)
	if admission != nil {
		admission.Finish(ctx, "canceled")
	}
	if !errors.Is(err, ErrInputChanged) {
		t.Fatal("old key epoch was admitted", err)
	}
	ticket.KeyEpoch = q.KeyEpoch
	id, _ = NewID()
	admission, err = client.BeginLaunch(ctx, id, ticket)
	if err != nil {
		t.Fatal("new epoch rejected", err)
	}
	if err = admission.Finish(ctx, "spawned"); err != nil {
		t.Fatal(err)
	}
}

func TestStorageMutationDrainsActualAdmittedOutcome(t *testing.T) {
	client, master, store, p, _, ticket := rotationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, _ := NewID()
	admission, err := client.BeginLaunch(ctx, id, ticket)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := store.RotateProfile(ctx, p.Name, master); done <- err }()
	// Wait on a real bounded attempt: the shared physical lease must prevent
	// exclusive mutation until the launcher records its actual outcome.
	short, stop := context.WithTimeout(ctx, 40*time.Millisecond)
	if release, err := AcquireLaunchMutation(short, client.Directory); err == nil {
		release()
		t.Fatal("mutation passed live launcher")
	}
	stop()
	select {
	case err := <-done:
		t.Fatal("rotation completed before spawn outcome", err)
	default:
	}
	if _, err = client.Status(ctx); err != nil {
		t.Fatal("session status blocked by mutation", err)
	}
	if err = admission.Finish(ctx, "spawned"); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUnknownAdmissionBlocksStorageMutation(t *testing.T) {
	client, master, store, p, _, ticket := rotationFixture(t)
	ctx := context.Background()
	id, _ := NewID()
	admission, err := client.BeginLaunch(ctx, id, ticket)
	if err != nil {
		t.Fatal(err)
	}
	if err = admission.Finish(ctx, "unknown"); !errors.Is(err, ErrLockPending) {
		t.Fatal(err)
	}
	if _, err = store.RotateProfile(ctx, p.Name, master); !errors.Is(err, ErrLockPending) {
		t.Fatal("unknown launch allowed rotation", err)
	}
	current, currentKey, err := store.ReadWithMaster(ctx, p.Name, master)
	defer currentKey.Destroy()
	if err != nil {
		t.Fatal(err)
	}
	if current.KeyEpoch != p.KeyEpoch {
		t.Fatal("blocked rotation changed storage")
	}
}

func TestRestoredStorageGenerationRejectsOldPreparedAdmission(t *testing.T) {
	client, master, store, p, key, ticket := rotationFixture(t)
	ctx := context.Background()
	restored, err := p.clone()
	if err != nil {
		t.Fatal(err)
	}
	restored.Generation, err = NewID()
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Sign(key); err != nil {
		t.Fatal(err)
	}
	catalog := Catalog{Format: StorageVersion, VaultID: store.VaultID, Entries: []CatalogEntry{entryFor(restored)}}
	if err = catalog.Sign(master); err != nil {
		t.Fatal(err)
	}
	cb, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := restored.Encode()
	if err != nil {
		t.Fatal(err)
	}
	operation, _ := NewID()
	result, err := (ForwardStore{Root: store.Root, VaultID: store.VaultID, Master: master}).Replace(ctx, operation, strings.Repeat("a", 64), map[string][]byte{catalogRelative: cb, "profiles/" + profilekey.CanonicalFilename(p.Name): pb})
	if err != nil || !result.Committed {
		t.Fatal("generation replacement", err)
	}
	id, _ := NewID()
	admission, err := client.BeginLaunch(ctx, id, ticket)
	if admission != nil {
		admission.Finish(ctx, "canceled")
	}
	if !errors.Is(err, ErrInputChanged) {
		t.Fatal("old restored generation admitted", err)
	}
	ticket.StorageGeneration = restored.Generation
	id, _ = NewID()
	admission, err = client.BeginLaunch(ctx, id, ticket)
	if err != nil {
		t.Fatal(err)
	}
	if err = admission.Finish(ctx, "canceled"); err != nil {
		t.Fatal(err)
	}
}
