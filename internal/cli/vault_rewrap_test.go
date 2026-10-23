package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestVaultRewrapPublicPassphraseLifecycle(t *testing.T) {
	app := testApp(t)
	retireVaultTestSession(t, app)
	ctx := context.Background()
	oldText, newText := "synthetic-rewrap-original", "synthetic-rewrap-replacement"
	if code, _, diagnostic := invoke(t, app, oldText, "vault", "init", "--backend", "passphrase", "--passphrase-stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "synthetic-untouched-secret", "secret", "set", "TOKEN", "--profile", "rewrap", "--stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	dir, _ := app.vaultDirectory()
	slotBefore, err := (vault.KeyFileStore{Directory: dir}).Read()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := app.dataDirectory()
	profiles := map[string][]byte{}
	err = filepath.WalkDir(data, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			body, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			profiles[path] = body
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	passFile := filepath.Join(privateTempDir(t), "new-passphrase")
	if err = os.WriteFile(passFile, []byte(newText), 0600); err != nil {
		t.Fatal(err)
	}
	request := "12345678-1234-4234-8234-123456789ABC"
	args := []string{"vault", "rewrap", "--from-backend", "passphrase", "--backend", "passphrase", "--request-id", request, "--current-passphrase-stdin", "--new-passphrase-file", passFile}
	// A warm cache must not substitute for original-protector authentication.
	if code, _, diagnostic := invoke(t, app, "wrong-original", args...); code != 3 || !strings.Contains(diagnostic, "integrity_failed") {
		t.Fatal(code, diagnostic)
	}
	code, out, diagnostic := invoke(t, app, oldText, args...)
	if code != 0 || diagnostic != "" {
		t.Fatal(code, diagnostic)
	}
	var result struct {
		Data struct {
			Changed, Replayed bool
			RequestID         string `json:"request_id"`
			Item              vault.SessionStatus
		}
	}
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Data.Changed || result.Data.Replayed || result.Data.RequestID != strings.ToLower(request) || result.Data.Item.State != vault.Locked {
		t.Fatal("wrong change response", out)
	}
	slotAfter, err := (vault.KeyFileStore{Directory: dir}).Read()
	if err != nil {
		t.Fatal(err)
	}
	if slotAfter.VaultID != slotBefore.VaultID || bytes.Equal(slotBefore.Wrapped, slotAfter.Wrapped) {
		t.Fatal("did not rewrap same root identity")
	}
	for path, before := range profiles {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rewrap rewrote profile", path, err)
		}
	}
	if code, _, diagnostic = invoke(t, app, oldText, "vault", "unlock", "--passphrase-stdin"); code != 3 {
		t.Fatal("old passphrase accepted", code, diagnostic)
	}
	if code, _, diagnostic = invoke(t, app, newText, "vault", "unlock", "--passphrase-stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	client, err := app.keySessionClient(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	key, err := client.Use(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, profileKey, err := (vault.FileStore{Root: filepath.Dir(dir), VaultID: slotAfter.VaultID, CatalogKey: key}).ReadWithMaster(ctx, "rewrap", key)
	key.Destroy()
	if err != nil {
		t.Fatal(err)
	}
	env, _, err := p.Environment("", profileKey, time.Now())
	profileKey.Destroy()
	if err != nil || env["TOKEN"] != "synthetic-untouched-secret" {
		t.Fatal("rewrap changed data", err)
	}
	// A replay authenticates the installed target without requiring old input.
	replay := []string{"vault", "rewrap", "--from-backend", "passphrase", "--backend", "passphrase", "--request-id", strings.ToLower(request), "--new-passphrase-file", passFile}
	code, out, diagnostic = invoke(t, app, "", replay...)
	if code != 0 || !strings.Contains(out, `"replayed":true`) || !strings.Contains(out, `"changed":true`) {
		t.Fatal(code, out, diagnostic)
	}
	if code, _, diagnostic = invoke(t, app, "", "vault", "lock"); code != 0 {
		t.Fatal(diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "", replay...)
	if code != 0 || vaultStatusResult(t, out).State != vault.LockedManual {
		t.Fatal("replay cleared manual intent", code, out, diagnostic)
	}
	for _, body := range []string{out, diagnostic} {
		if strings.Contains(body, oldText) || strings.Contains(body, newText) {
			t.Fatal("passphrase leaked")
		}
	}
	// Receipt publication precedes pending cleanup. Recovery must route to the
	// exact rewrap before asking a manually locked session for any key.
	receipt, err := os.ReadFile(filepath.Join(dir, "rewrap-"+strings.ReplaceAll(strings.ToLower(request), "-", "")+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "rewrap-pending.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostic = invoke(t, app, "", "vault", "status")
	status := vaultStatusResult(t, out)
	if code != 0 || status.Rewrap == nil || !status.Rewrap.SlotPublished || status.Rewrap.IntegrityVerified {
		t.Fatal("pending status missing or overstates verification", out, diagnostic)
	}
	code, _, diagnostic = invoke(t, app, "", "vault", "recover")
	if code != 3 || !strings.Contains(diagnostic, strings.ToLower(request)) || !strings.Contains(diagnostic, `"rewrap"`) || strings.Contains(diagnostic, `"unlock"`) {
		t.Fatal("recovery did not preserve pending operation", code, diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "", replay...)
	if code != 0 || vaultStatusResult(t, out).State != vault.LockedManual || vaultStatusResult(t, out).Rewrap != nil {
		t.Fatal("pending replay did not finish without unlocking", code, out, diagnostic)
	}
}

func TestVaultRewrapRejectsInputBeforeReadingCredentials(t *testing.T) {
	app := testApp(t)
	app.userPaths = func() (paths.Directories, error) { return paths.Directories{IsolatedRoot: privateTempDir(t)}, nil }
	base := []string{"vault", "rewrap", "--from-backend", "passphrase", "--backend", "passphrase", "--request-id", "00000000-0000-4000-8000-000000000012"}
	for _, extra := range [][]string{{"--current-passphrase-stdin", "--new-passphrase-stdin"}, {"--new-passphrase-stdin", "--new-passphrase-file", "missing"}} {
		code, _, diagnostic := invoke(t, app, "should-not-be-consumed", append(append([]string{}, base...), extra...)...)
		if code != 2 || !strings.Contains(diagnostic, "invalid_argument") {
			t.Fatal(code, diagnostic)
		}
	}
	code, _, diagnostic := invoke(t, app, "", "vault", "rewrap", "--from-backend", "passphrase", "--backend", "native", "--request-id", "00000000-0000-4000-8000-000000000012")
	if code != 3 || !strings.Contains(diagnostic, "isolated_operation_forbidden") {
		t.Fatal(code, diagnostic)
	}
}

func TestVaultRewrapPromptConfirmsNewPassphrase(t *testing.T) {
	app := testApp(t)
	var prompts []string
	answers := []string{"synthetic-old", "synthetic-new", "different"}
	app.passphrasePrompt = func(_ context.Context, _ IO, label string) (string, *protocol.Error) {
		prompts = append(prompts, label)
		s := answers[0]
		answers = answers[1:]
		return s, nil
	}
	old, e := app.rewrapPassphrase(context.Background(), IO{}, map[string]string{}, "current", true, false)
	if e != nil {
		t.Fatal(e)
	}
	old.Destroy()
	_, e = app.rewrapPassphrase(context.Background(), IO{}, map[string]string{}, "new", true, true)
	if e == nil || e.Code != "passphrase_mismatch" || len(prompts) != 3 {
		t.Fatal("new passphrase was not confirmed", e, prompts)
	}
}
