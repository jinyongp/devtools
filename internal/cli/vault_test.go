package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestVaultInitializationErrorKeepsRecoveryAndOtherErrors(t *testing.T) {
	for _, kind := range []string{"uninitialized", "protected_without_key", "existing_key", "passphrase", "denied", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			directory := filepath.Join(privateTempDir(t), "vault")
			ctx := context.Background()
			backend := "secret-service"
			cause := fmt.Errorf("native setup: %w", vault.ErrUnavailable)
			switch kind {
			case "protected_without_key":
				profiles := filepath.Join(filepath.Dir(directory), "profiles")
				if err := os.MkdirAll(profiles, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(profiles, "vault-catalog.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "existing_key":
				id, err := vault.NewID()
				if err != nil {
					t.Fatal(err)
				}
				pass := vault.NewMaterial([]byte("synthetic-existing-vault"))
				defer pass.Destroy()
				master, err := (vault.KeyFileStore{Directory: directory}).Initialize(ctx, id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
				if err != nil {
					t.Fatal(err)
				}
				defer master.Destroy()
			case "passphrase":
				backend = "passphrase"
			case "denied":
				cause = vault.ErrDenied
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			e := vaultInitializationError(ctx, directory, backend, cause)
			body, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.Contains(string(body), `"--backend","passphrase"`), kind == "uninitialized"; got != want {
				t.Fatalf("unsafe or missing initialization remedy: %s", body)
			}
			if kind == "uninitialized" && (e.Code != "key_backend_unavailable" || !strings.Contains(e.Message, "Secret Service")) {
				t.Fatalf("missing backend diagnosis: %s", body)
			}
		})
	}
}

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

// New secret writes require explicit vault setup. Shared command tests use the
// real reusable passphrase session instead of preserving plaintext behavior.
func initializeTestVault(t *testing.T, app *App) {
	t.Helper()
	retireVaultTestSession(t, app)
	code, _, diagnostic := invoke(t, app, "synthetic-shared-command-test-unlock\n", "vault", "init", "--backend", "passphrase", "--passphrase-stdin")
	if code != 0 {
		t.Fatal("initialize protected command fixture", code, diagnostic)
	}
}

func TestProtectedValueCLIReusesSessionAndKeepsLockedReadsPassive(t *testing.T) {
	app := testApp(t)
	directory, e := app.dataDirectory()
	if e != nil {
		t.Fatal(e)
	}
	if code, _, diagnostic := invoke(t, app, "", "var", "set", "URL", "--value", "https://example.invalid", "--profile", "app"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(directory), "vault", "key-slot.json")); !os.IsNotExist(err) {
		t.Fatal("public CLI initialized vault")
	}
	if code, _, diagnostic := invoke(t, app, "synthetic-required-protection", "sec", "set", "TOKEN", "--stdin", "--profile", "app"); code != 3 || !strings.Contains(diagnostic, "key_missing") {
		t.Fatal("secret write did not require protection", code, diagnostic)
	}
	initializeTestVault(t, app)
	if code, _, diagnostic := invoke(t, app, "synthetic-protected-command-secret", "sec", "set", "TOKEN", "--stdin", "--profile", "app"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "", "vault", "lock"); code != 0 {
		t.Fatal(diagnostic)
	}
	before := outputData(t, app, []string{"vault", "status"})
	for _, args := range [][]string{{"var", "get", "URL", "--profile", "app"}, {"sec", "list", "--profile", "app"}, {"profile", "inspect", "app"}} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 0 || strings.Contains(out+diagnostic, "synthetic-protected-command-secret") {
			t.Fatal("locked metadata read", args, code, diagnostic)
		}
	}
	projectRoot := privateTempDir(t)
	config := "profile='app'\n[commands.check]\ninject=true\nexec=" + fixtureExec(t, "true") + "\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	code, diagnosis, diagnostic := invoke(t, app, "", "doctor", "check", "--dir", projectRoot)
	var report struct {
		Data struct {
			Ready bool `json:"ready"`
		} `json:"data"`
	}
	if code != 0 || json.Unmarshal([]byte(diagnosis), &report) != nil || report.Data.Ready || !strings.Contains(diagnosis, "vault_session") {
		t.Fatal("doctor missed locked session", code, diagnostic)
	}
	after := outputData(t, app, []string{"vault", "status"})
	if mustJSON(t, before) != mustJSON(t, after) || after["item"].(map[string]any)["state"] != "locked_manual" {
		t.Fatal("metadata read changed locked session")
	}
	for _, args := range [][]string{{"var", "set", "URL", "--value", "changed", "--profile", "app"}, append([]string{"run", "--profile", "app", "--"}, commandFixture(t, "true")...)} {
		code, _, diagnostic := invoke(t, app, "", args...)
		if code != 3 || !strings.Contains(diagnostic, "vault_locked") {
			t.Fatal("locked authenticated operation accepted", args, code, diagnostic)
		}
	}
}

type observedSecretReader struct{ read bool }

func (r *observedSecretReader) Read([]byte) (int, error) { r.read = true; return 0, io.EOF }
func TestSecretInputIsNotConsumedBeforeProtectionIsAvailable(t *testing.T) {
	app := testApp(t)
	input := &observedSecretReader{}
	var out, diagnostic bytes.Buffer
	code := app.Run(context.Background(), []string{"sec", "set", "TOKEN", "--stdin", "--profile", "app"}, IO{In: input, Out: &out, Err: &diagnostic})
	if code != 3 || !strings.Contains(diagnostic.String(), "key_missing") || input.read {
		t.Fatal("unavailable protection consumed secret input", code, input.read)
	}
}

func TestSecretFrontendPrefixWarningKeepsValuePrivate(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	code, out, diagnostic := invoke(t, app, "synthetic-frontend-secret-canary", "sec", "set", "VITE_TOKEN", "--profile", "app", "--stdin")
	if code != 0 || !strings.Contains(out, "frontend_public_prefix") || strings.Contains(out+diagnostic, "synthetic-frontend-secret-canary") {
		t.Fatal("frontend warning missing or private value echoed", code)
	}
	if code, _, _ := invoke(t, app, "", "var", "get", "VITE_TOKEN", "--profile", "app"); code != 3 {
		t.Fatal("warning reclassified secret as public")
	}
}
