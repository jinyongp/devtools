package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
