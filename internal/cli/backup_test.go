package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/jinyongp/devtools/internal/archivekey"
	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestBackupPassphraseWithoutSetupAndReplacementRecovery(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	for _, variable := range presentation.AgentVariables {
		t.Setenv(variable, "")
	}
	app := testApp(t)
	const passphrase = "fixture-backup-passphrase-only"
	prompts := 0
	app.passphrasePrompt = func(_ context.Context, _ IO, label string) (string, *protocol.Error) {
		if !strings.Contains(strings.ToLower(label), "backup") {
			t.Fatal("prompt does not identify the backup passphrase")
		}
		prompts++
		return passphrase, nil
	}
	outputData(t, app, []string{"var", "set", "MODE", "--profile", "source", "--value", "archived"})
	created := outputData(t, app, []string{"backup", "create"})
	if prompts != 2 {
		t.Fatalf("create requested %d passphrase entries", prompts)
	}
	archive := created["item"].(map[string]any)["path"].(string)
	engine, failure := app.backupEngine()
	if failure != nil {
		t.Fatal(failure)
	}
	if filepath.Dir(archive) != filepath.Join(engine.Data, "backups") {
		t.Fatalf("unexpected default backup directory: %s", archive)
	}
	if format, err := backup.ArchiveEncryptionMode(archive); err != nil || format != archivekey.Passphrase {
		t.Fatalf("archive mode = %q, %v", format, err)
	}
	if _, err := os.Stat(filepath.Join(engine.Config, "backup.json")); !os.IsNotExist(err) {
		t.Fatalf("passphrase backup changed backup configuration: %v", err)
	}
	outputData(t, app, []string{"backup", "inspect", "--file", archive})
	if prompts != 3 {
		t.Fatalf("inspect did not auto-detect passphrase mode")
	}
	outputData(t, app, []string{"var", "set", "MODE", "--profile", "target", "--value", "before-replace"})
	base := []string{"backup", "restore", "--file", archive, "--profile", "source", "--as", "target", "--replace"}
	preview := outputData(t, app, base)
	if prompts != 4 {
		t.Fatal("restore preview did not auto-detect passphrase mode")
	}
	apply := append(base, "--apply", preview["digest"].(string), "--passphrase-stdin")
	code, out, diagnostic := invoke(t, app, passphrase+"\n", apply...)
	if code != 0 || strings.Contains(out+diagnostic, passphrase) {
		t.Fatalf("restore apply exit=%d, unexpected diagnostic or passphrase disclosure", code)
	}
	var envelope struct{ Data backup.Plan }
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || !envelope.Data.Applied || envelope.Data.SafetyBackup == "" {
		t.Fatalf("missing applied restore or safety backup: %v", err)
	}
	if got := outputData(t, app, []string{"var", "get", "MODE", "--profile", "target"}); got["value"] != "archived" {
		t.Fatalf("restored value = %#v", got)
	}
	recover := []string{"backup", "restore", "--file", envelope.Data.SafetyBackup, "--profile", "target", "--as", "recovered"}
	recovery := outputData(t, app, recover)
	outputData(t, app, append(recover, "--apply", recovery["digest"].(string)))
	if got := outputData(t, app, []string{"var", "get", "MODE", "--profile", "recovered"}); got["value"] != "before-replace" {
		t.Fatalf("safety backup did not preserve original target: %#v", got)
	}
}

func TestBackupUnattendedLegacyRecipientCannotWriteArchive(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	app := testApp(t)
	engine, failure := app.backupEngine()
	if failure != nil {
		t.Fatal(failure)
	}
	legacy, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	recipient := legacy.Recipient().String()
	directory := filepath.Join(root, "legacy-backups")
	config, err := json.Marshal(backup.Config{Directory: directory, Recipient: recipient})
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.PrivateDir(engine.Config); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Write(filepath.Join(engine.Config, "backup.json"), config); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "must-not-exist.age")
	for _, args := range [][]string{
		{"backup", "create"},
		{"backup", "create", "--output", output, "--recipient", recipient},
	} {
		exit, stdout, stderr := invoke(t, app, "", args...)
		if exit != 3 || stdout != "" || !strings.Contains(stderr, `"code":"legacy_backup_recipient"`) {
			t.Fatalf("legacy write did not fail closed: %d %s %s", exit, stdout, stderr)
		}
	}
	for _, path := range []string{output, directory} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("failed legacy backup created %s: %v", path, err)
		}
	}
	// Execute the unattended retry using the same configured destination.
	exit, stdout, stderr := invoke(t, app, "synthetic-legacy-upgrade-passphrase\n", "backup", "create", "--passphrase-stdin")
	if exit != 0 {
		t.Fatalf("passphrase retry failed: %s", stderr)
	}
	var result struct {
		Data struct{ Item backup.CreateResult }
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	path := result.Data.Item.Path
	if mode, err := backup.ArchiveEncryptionMode(path); err != nil || mode != archivekey.Passphrase || filepath.Dir(path) != directory {
		t.Fatalf("passphrase retry destination or encryption mode: %q %q %v", path, mode, err)
	}
}

func TestBackupStatusAndDirectRecipientCLI(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	app := testApp(t)

	status := outputData(t, app, []string{"backup", "status"})
	item := status["item"].(map[string]any)
	if item["configured"] != false || item["directory"] != "" || item["recipient"] != "" {
		t.Fatalf("unexpected unconfigured backup status: %#v", item)
	}

	identity := filepath.Join(root, "identity.txt")
	recipientPath := filepath.Join(root, "recipient.txt")
	if code, _, diagnostic := invoke(t, app, "", "backup", "keygen", "--identity-file", identity, "--recipient-file", recipientPath); code != 0 {
		t.Fatal(diagnostic)
	}
	identityBytes, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	recipientBytes, err := os.ReadFile(recipientPath)
	if err != nil {
		t.Fatal(err)
	}
	identityValue := strings.TrimSpace(string(identityBytes))
	recipientValue := strings.TrimSpace(string(recipientBytes))
	if !strings.HasPrefix(recipientValue, "age1pq1") || !strings.HasPrefix(identityValue, "AGE-SECRET-KEY-PQ-1") {
		t.Fatal("backup keygen did not generate hybrid keys")
	}
	if code, _, diagnostic := invoke(t, app, "", "var", "set", "VALUE", "--profile", "app", "--value", "portable"); code != 0 {
		t.Fatal(diagnostic)
	}

	directArchive := filepath.Join(root, "direct.age")
	created := outputData(t, app, []string{"backup", "create", "--profile", "app", "--output", directArchive, "--recipient", recipientValue})
	if created["changed"] != true || created["item"].(map[string]any)["path"] != directArchive {
		t.Fatalf("direct recipient backup failed: %#v", created)
	}
	if code, out, diagnostic := invoke(t, app, "", "backup", "create", "--profile", "app", "--output", directArchive, "--recipient", recipientValue); code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"backup_error\"") {
		t.Fatalf("backup output collision contract changed: code=%d out=%s err=%s", code, out, diagnostic)
	}
	if code, out, diagnostic := invoke(t, app, "", "backup", "create", "--profile", "app", "--output", filepath.Join(root, "conflict.age"), "--recipient", recipientValue, "--recipient-file", recipientPath); code != 2 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
		t.Fatalf("recipient conflict should fail: code=%d out=%s err=%s", code, out, diagnostic)
	}

	backupDirectory := filepath.Join(root, "backups")
	if data := outputData(t, app, []string{"backup", "configure", "--directory", backupDirectory, "--recipient-file", recipientPath}); data["changed"] != true {
		t.Fatalf("backup configure failed: %#v", data)
	}
	code, out, diagnostic := invoke(t, app, "", "backup", "status")
	if code != 0 || diagnostic != "" || strings.Contains(out, identityValue) {
		t.Fatalf("backup status failed or leaked identity: code=%d out=%s err=%s", code, out, diagnostic)
	}
	status = outputData(t, app, []string{"backup", "status"})
	item = status["item"].(map[string]any)
	if item["configured"] != true || item["directory"] != backupDirectory || item["recipient"] != recipientValue {
		t.Fatalf("unexpected configured backup status: %#v", item)
	}
	fallback := outputData(t, app, []string{"backup", "create", "--profile", "app"})
	fallbackPath, _ := fallback["item"].(map[string]any)["path"].(string)
	if fallback["changed"] != true || filepath.Dir(fallbackPath) != backupDirectory {
		t.Fatalf("configured recipient fallback failed: %#v", fallback)
	}
	if mode, err := backup.ArchiveEncryptionMode(fallbackPath); err != nil || mode != archivekey.Hybrid {
		t.Fatalf("configured backup mode = %q, %v", mode, err)
	}

	externalIdentity := filepath.Join(root, "external-identity.txt")
	externalRecipientPath := filepath.Join(root, "external-recipient.txt")
	if code, _, diagnostic := invoke(t, app, "", "backup", "keygen", "--identity-file", externalIdentity, "--recipient-file", externalRecipientPath); code != 0 {
		t.Fatal(diagnostic)
	}
	externalRecipientBytes, err := os.ReadFile(externalRecipientPath)
	if err != nil {
		t.Fatal(err)
	}
	externalRecipient := strings.TrimSpace(string(externalRecipientBytes))
	explicit := outputData(t, app, []string{"backup", "create", "--profile", "app", "--recipient", externalRecipient})
	explicitPath := explicit["item"].(map[string]any)["path"].(string)
	if filepath.Dir(explicitPath) != backupDirectory {
		t.Fatalf("explicit recipient did not reuse configured output directory: %#v", explicit)
	}
	if code, _, diagnostic := invoke(t, app, "", "backup", "inspect", "--file", explicitPath, "--identity-file", externalIdentity); code != 0 {
		t.Fatalf("explicit recipient was overwritten by configured recipient: %s", diagnostic)
	}
	if code, out, diagnostic := invoke(t, app, "", "backup", "inspect", "--file", explicitPath, "--identity-file", identity); code != 3 || out != "" || !strings.Contains(diagnostic, "\"code\":\"invalid_backup\"") {
		t.Fatalf("configured identity unexpectedly decrypted explicit-recipient backup: code=%d out=%s err=%s", code, out, diagnostic)
	}
}
