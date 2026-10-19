package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func vaultStatusResult(t *testing.T, output string) vault.SessionStatus {
	t.Helper()
	var result struct {
		Data struct {
			Item vault.SessionStatus `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	return result.Data.Item
}
func retireVaultTestSession(t *testing.T, app *App) {
	t.Helper()
	directory, e := app.vaultDirectory()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		client, err := vault.OpenSessionClient(directory)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = client.Retire(ctx); err != nil {
			t.Errorf("retire isolated test session: %v", err)
		}
	})
}
func TestVaultCommandsMigrateLockUnlockAndPassiveStatus(t *testing.T) {
	app := testApp(t)
	retireVaultTestSession(t, app)
	ctx := context.Background()
	directory, e := app.dataDirectory()
	if e != nil {
		t.Fatal(e)
	}
	legacy := values.Store{Directory: directory, Profile: "app"}
	if _, e = legacy.Update(ctx, func(s *values.State) (bool, *protocol.Error) {
		return s.Set(values.Secret, "TOKEN", "", "synthetic-vault-cli-migration")
	}); e != nil {
		t.Fatal(e)
	}
	code, out, diagnostic := invoke(t, app, "", "vault", "status")
	if code != 0 || vaultStatusResult(t, out).State != vault.Uninitialized {
		t.Fatal("passive initial status", code, diagnostic)
	}
	// No passphrase input must fail immediately, without initializing storage.
	code, _, diagnostic = invoke(t, app, "", "vault", "init", "--backend", "passphrase")
	if code != 3 || !strings.Contains(diagnostic, "passphrase_required") {
		t.Fatal("noninteractive prompt did not fail", code, diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "synthetic-vault-cli-passphrase\n", "vault", "init", "--backend", "passphrase", "--passphrase-stdin")
	if code != 0 || vaultStatusResult(t, out).State != vault.Unlocked {
		t.Fatal("vault initialization", code, diagnostic)
	}
	before := vaultStatusResult(t, out)
	code, out, diagnostic = invoke(t, app, "", "vault", "status")
	after := vaultStatusResult(t, out)
	if code != 0 || before.IdleExpiresAt == nil || after.IdleExpiresAt == nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("status touched session idle time", code, diagnostic)
	}
	client, err := app.keySessionClient(ctx, filepath.Join(filepath.Dir(directory), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	master, err := client.Use(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files := vault.FileStore{Root: filepath.Dir(directory), VaultID: after.VaultID, CatalogKey: master}
	p, key, err := files.ReadWithMaster(ctx, "app", master)
	if err != nil {
		master.Destroy()
		t.Fatal(err)
	}
	env, _, err := p.Environment("", key, time.Now())
	key.Destroy()
	master.Destroy()
	if err != nil || env["TOKEN"] != "synthetic-vault-cli-migration" {
		t.Fatal("migration changed legacy secret", err)
	}
	if _, e = legacy.Read(ctx); e == nil || e.Code != "unsupported_storage" {
		t.Fatal("old writer not denied", e)
	}
	code, out, diagnostic = invoke(t, app, "", "vault", "lock")
	if code != 0 || vaultStatusResult(t, out).State != vault.LockedManual {
		t.Fatal("manual lock", code, diagnostic)
	}
	code, _, diagnostic = invoke(t, app, "wrong-synthetic-passphrase\n", "vault", "unlock", "--passphrase-stdin")
	if code != 3 {
		t.Fatal("wrong passphrase accepted", code, diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "synthetic-vault-cli-passphrase\n", "vault", "unlock", "--passphrase-stdin")
	if code != 0 || vaultStatusResult(t, out).State != vault.Unlocked {
		t.Fatal("explicit unlock", code, diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "", "vault", "recover")
	if code != 0 || vaultStatusResult(t, out).State != vault.Unlocked {
		t.Fatal("recovery of healthy store", code, diagnostic)
	}
	code, _, diagnostic = invoke(t, app, "", "vault", "init", "--backend", "passphrase")
	if code != 3 || !strings.Contains(diagnostic, "vault_already_initialized") {
		t.Fatal("second initialization consumed input or reset vault", code, diagnostic)
	}
}

func TestVaultRecoverCompletesSetupBeforeMigrationAndRejectsCorruption(t *testing.T) {
	app := testApp(t)
	retireVaultTestSession(t, app)
	ctx := context.Background()
	directory, e := app.vaultDirectory()
	if e != nil {
		t.Fatal(e)
	}
	id, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-interrupted-setup"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: directory}).Initialize(ctx, id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	code, _, diagnostic := invoke(t, app, "synthetic-interrupted-setup\n", "vault", "unlock", "--passphrase-stdin")
	if code != 0 {
		t.Fatal("unlock partial setup", code, diagnostic)
	}
	code, _, diagnostic = invoke(t, app, "", "vault", "recover")
	if code != 0 {
		t.Fatal("recover partial setup", code, diagnostic)
	}
	catalog := filepath.Join(filepath.Dir(directory), "profiles", "vault-catalog.json")
	raw, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["tag"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(catalog, raw, 0600); err != nil {
		t.Fatal(err)
	}
	code, _, diagnostic = invoke(t, app, "", "vault", "recover")
	if code == 0 || !strings.Contains(diagnostic, "integrity_failed") {
		t.Fatal("corrupt catalog reported recovered", code, diagnostic)
	}
}
