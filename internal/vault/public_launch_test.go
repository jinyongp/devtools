package vault

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/maintenance"
	"path/filepath"
	"testing"
	"time"
)

func TestPublicLaunchUnknownBlocksProtectionUntilKnownOutcome(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "vault")
	id, _ := NewID()
	admission, err := BeginPublicLaunch(ctx, dir, id, PublicLaunchTicket{Profile: "app"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate loss of the launcher's physical lease. The durable unknown is
	// deliberately not treated as a known failure just because the lease is gone.
	admission.release()
	admission.release = nil
	if err = DrainLaunches(ctx, dir); !errors.Is(err, ErrLockPending) {
		t.Fatal("public unknown ignored", err)
	}
	vaultID, _ := NewID()
	wrapping, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer wrapping.Destroy()
	protector := sessionTestProtector{id: vaultID, key: wrapping}
	short, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	master, err := (KeyFileStore{Directory: dir}).Initialize(short, vaultID, "dpapi", protector)
	master.Destroy()
	if !errors.Is(err, ErrLockPending) {
		t.Fatal("root setup bypassed unknown public launch", err)
	}
	if _, err = (KeyFileStore{Directory: dir}).Read(); !errors.Is(err, ErrKeyMissing) {
		t.Fatal("failed root setup published a slot", err)
	}
}

func TestSessionAdmissionAuthenticatesPublicProfileAndPromotion(t *testing.T) {
	client, ticket := launchFixture(t)
	ctx := context.Background()
	master, err := client.Use(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	store := FileStore{Root: filepath.Dir(client.Directory), VaultID: ticket.VaultID, CatalogKey: master}
	if err = store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := NewProfile("app")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Set("URL", Public, "", "https://example.invalid", nil); err != nil {
		t.Fatal(err)
	}
	if err = store.Create(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	ticket.Profile = p.Name
	ticket.ProfileID = p.ID
	ticket.StorageGeneration = p.Generation
	ticket.KeyEpoch = 0
	id, _ := NewID()
	admission, err := client.BeginLaunch(ctx, id, ticket)
	if err != nil {
		t.Fatal("public session admission", err)
	}
	if err = admission.Finish(ctx, "spawned"); err != nil {
		t.Fatal(err)
	}
	// Profile protection changes invalidate an earlier public delivery even when
	// its logical public identities and storage generation remain unchanged.
	key, err := p.Seal(ticket.VaultID, master)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	finish, err := AcquireLaunchMutation(ctx, client.Directory)
	if err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireExclusive(ctx, store.Root)
	if err != nil {
		finish()
		t.Fatal(err)
	}
	err = store.PublishManagedHeld(ctx, p, key)
	release()
	finish()
	if err != nil {
		t.Fatal(err)
	}
	id, _ = NewID()
	admission, err = client.BeginLaunch(ctx, id, ticket)
	if admission != nil {
		admission.Finish(ctx, "canceled")
	}
	if !errors.Is(err, ErrInputChanged) {
		t.Fatal("old public ticket admitted after promotion", err)
	}
	ticket.KeyEpoch = p.KeyEpoch
	id, _ = NewID()
	admission, err = client.BeginLaunch(ctx, id, ticket)
	if err != nil {
		t.Fatal("promoted ticket rejected", err)
	}
	if err = admission.Finish(ctx, "spawned"); err != nil {
		t.Fatal(err)
	}
}
