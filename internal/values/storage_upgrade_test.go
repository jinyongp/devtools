package values

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/vault"
)

func upgradeReceiptFixture(t *testing.T, root string, master *vault.Material, vaultID string) {
	t.Helper()
	releaseLaunch, err := vault.AcquireLaunchMutation(context.Background(), filepath.Join(root, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLaunch()
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := migrateLegacyReceiptsHeld(context.Background(), root, master, vaultID); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitStorageUpgradePreservesPublicLayersAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	directory := filepath.Join(root, "profiles")
	state := newState("app")
	state.Set(Variable, "URL", "", "https://example.invalid")
	state.CreateEnv("preview")
	state.Set(Variable, "URL", "preview", "https://preview.invalid")
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Write(profilekey.LegacyPath(directory, "app"), raw); err != nil {
		t.Fatal(err)
	}
	release()
	engine := storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter(root)}}
	before, condition := engine.Inspect(ctx)
	if condition != nil || before.Ready {
		t.Fatal(before, condition)
	}
	report, condition := engine.Fix(ctx)
	if condition != nil || !report.Ready || !report.Changed {
		t.Fatal(report, condition)
	}
	store := ModernStore{Directory: directory, Profile: "app"}
	got, condition := store.ReadForQuery(ctx)
	if condition != nil {
		t.Fatal(condition)
	}
	for env, want := range map[string]string{"": "https://example.invalid", "preview": "https://preview.invalid"} {
		value, _, condition := got.GetVariable("URL", env)
		if condition != nil || value != want {
			t.Fatal(env, value, condition)
		}
	}
	canonical, err := os.ReadFile(profilekey.CanonicalPath(directory, "app"))
	if err != nil {
		t.Fatal(err)
	}
	report, condition = engine.Fix(ctx)
	if condition != nil || !report.Ready || report.Changed || report.Effects != "none" {
		t.Fatal(report, condition)
	}
	again, err := os.ReadFile(profilekey.CanonicalPath(directory, "app"))
	if err != nil || !bytes.Equal(canonical, again) {
		t.Fatal("repeat upgrade changed identities", err)
	}
	if marker, err := profilekey.ReadMarker(profilekey.LegacyPath(directory, "app"), "app"); err != nil || !marker {
		t.Fatal("old live payload retained", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "vault", "key-slot.json")); !os.IsNotExist(err) {
		t.Fatal("public upgrade initialized vault", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".storage-upgrade", "recovery"))
	if err != nil || len(entries) != 1 {
		t.Fatal("missing private recovery image", err)
	}
}

func TestStorageUpgradeWithoutProtectionPreservesLegacySecrets(t *testing.T) {
	root := privateTempDir(t)
	directory := filepath.Join(root, "profiles")
	raw := []byte(`{"version":1,"profile":"old","envs":{},"keys":{"TOKEN":{"kind":"secret","common":"private-legacy-sentinel","envs":{}}}}`)
	path := profilekey.LegacyPath(directory, "old")
	if err := maintenance.Write(path, raw); err != nil {
		t.Fatal(err)
	}
	report, condition := (storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter(root)}}).Fix(context.Background())
	if condition != nil || report.Ready || report.State != "blocked" {
		t.Fatal(report, condition)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "private-legacy-sentinel") || !strings.Contains(string(encoded), "key_missing") {
		t.Fatal("missing protection cause lost or plaintext exposed")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("failed migration changed plaintext source", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "vault", "key-slot.json")); !os.IsNotExist(err) {
		t.Fatal("migration chose protection", err)
	}
}

func TestStorageUpgradeColdVaultDoesNotStartSessionOrChangeSource(t *testing.T) {
	root, _, _, _ := vaultSnapshotFixture(t, 0)
	directory := filepath.Join(root, "profiles")
	raw := []byte(`{"version":1,"profile":"old","envs":{},"keys":{"TOKEN":{"kind":"secret","common":"private-cold-sentinel","envs":{}}}}`)
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := profilekey.Replacement(directory, "old", "values", raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Replace(root, files); err != nil {
		t.Fatal(err)
	}
	release()
	engine := storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter(root)}}
	report, condition := engine.Fix(context.Background())
	if condition != nil || report.Ready || report.State != "blocked" {
		t.Fatal(report, condition)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "private-cold-sentinel") || !strings.Contains(string(encoded), "vault_locked") {
		t.Fatal("cold cause lost or plaintext exposed")
	}
	got, err := os.ReadFile(profilekey.CanonicalPath(directory, "old"))
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("cold fix changed source", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "vault", "session.json")); !os.IsNotExist(err) {
		t.Fatal("fix started a session", err)
	}
}

func TestProfileAddressUpgradePreservesIdentityAndWarmIdle(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(fmt.Sprint(protected), func(t *testing.T) {
			ctx := context.Background()
			root := privateTempDir(t)
			var client *vault.SessionClient
			var idle *time.Time
			if protected {
				root, _, _, _ = vaultSnapshotFixture(t, 1)
				client = warmUpgradeSession(t, root)
				status, err := client.Status(ctx)
				if err != nil {
					t.Fatal(err)
				}
				idle = status.IdleExpiresAt
			} else {
				store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
				if _, condition := store.Update(ctx, func(editor *Editor) (bool, *protocol.Error) { return editor.Set(Variable, "URL", "", "public") }); condition != nil {
					t.Fatal(condition)
				}
			}
			directory := filepath.Join(root, "profiles")
			before, err := maintenance.Read(profilekey.CanonicalPath(directory, "app"), vault.MaxDocumentBytes)
			if err != nil {
				t.Fatal(err)
			}
			release, err := maintenance.AcquireExclusive(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			err = maintenance.Replace(root, map[string][]byte{profilekey.StateRelative("profiles", "app"): nil, profilekey.IdentityRelative("profiles", "app"): nil, profilekey.LegacyRelative("profiles", "app"): before})
			release()
			if err != nil {
				t.Fatal(err)
			}
			report, condition := (storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter(root)}}).Fix(ctx)
			if condition != nil || !report.Ready || !report.Changed {
				t.Fatal(report, condition)
			}
			after, err := maintenance.Read(profilekey.CanonicalPath(directory, "app"), vault.MaxDocumentBytes)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("address migration changed existing profile identities/revision", err)
			}
			if client != nil {
				status, err := client.Status(ctx)
				if err != nil || status.IdleExpiresAt == nil || idle == nil || !status.IdleExpiresAt.Equal(*idle) {
					t.Fatal("migration renewed key idle expiry", err)
				}
			}
		})
	}
}

func warmUpgradeSession(t *testing.T, root string) *vault.SessionClient {
	t.Helper()
	directory := filepath.Join(root, "vault")
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- vault.ServeKeySession(ctx, directory) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("fixture session did not stop")
		}
	})
	var client *vault.SessionClient
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		client, err = vault.OpenSessionClient(directory)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if client == nil {
		t.Fatal("fixture session did not start")
	}
	pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
	defer pass.Destroy()
	if err := client.Unlock(context.Background(), pass, false); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestStorageInspectDoesNotIgnoreFutureCurrentReceipt(t *testing.T) {
	root := privateTempDir(t)
	ctx := context.Background()
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	release()
	store := ModernStore{Profile: "app"}
	path := filepath.Join(root, store.receiptRelative("00000000-0000-4000-8000-000000000123"))
	body := []byte(`{"protected_material_format":999}`)
	if err := maintenance.Write(path, body); err != nil {
		t.Fatal(err)
	}
	engine := storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter(root)}}
	for _, fix := range []bool{false, true} {
		report, condition := engine.Inspect(ctx)
		if fix {
			report, condition = engine.Fix(ctx)
		}
		if condition != nil || report.Ready || report.State != "blocked" {
			t.Fatal(report, condition)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("future receipt overwritten", err)
	}
}
