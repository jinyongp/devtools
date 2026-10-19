package values

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func TestProtectedPublicProfileDoesNotClaimSealedManifestIntegrity(t *testing.T) {
	root, store, _, _ := vaultSnapshotFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := vault.NewProfile("public")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Set("URL", vault.Public, "", "synthetic-public-only-value", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "vault")
	finished := make(chan error, 1)
	go func() { finished <- vault.ServeKeySession(ctx, directory) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("owned public inspection fixture did not stop")
		}
	}()
	var client *vault.SessionClient
	for {
		client, err = vault.OpenSessionClient(directory)
		if err == nil {
			if _, err = client.Status(ctx); err == nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned session unavailable")
		case <-time.After(10 * time.Millisecond):
		}
	}
	pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
	defer pass.Destroy()
	if err := client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	snapshot, e := CaptureVaultSession(ctx, root, "public", "", true, time.Now(), client)
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Destroy()
	metadata := snapshot.Metadata()
	if metadata.Launch == nil || metadata.IntegrityVerified {
		t.Fatal("fixture did not distinguish root admission from unsigned profile")
	}
	delivered, err := NewInputVersion(metadata, "", InputSpec{Inject: true})
	if err != nil || delivered == nil || delivered.IntegrityVerified {
		t.Fatal("root admission was presented as value manifest integrity", err)
	}
	current, e := InspectInput(ctx, root, "public", "", InputSpec{Inject: true})
	if e != nil || current == nil || current.IntegrityVerified {
		t.Fatal("public-only profile misreported cryptographic integrity", e)
	}
	if same, known := SameInput(delivered, current); !same || !known {
		t.Fatal("unsigned public profile cannot be compared")
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, e := InspectInput(ctx, root, "public", "", InputSpec{Inject: true}); e == nil {
		t.Fatal("unsigned public profile bypassed protected root authentication")
	}
}
