package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProfileSourceFirstNonInteractivePassphraseRoundTrip(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	const passphrase = "NONINTERACTIVE-PASSPHRASE-CANARY"
	passphraseFile := filepath.Join(root, "passphrase.txt")
	if err := os.WriteFile(passphraseFile, []byte(passphrase+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	source := testApp(t)
	target := testApp(t)
	outputData(t, source, []string{"var", "set", "MODE", "--profile", "portable", "--value", "noninteractive"})
	archive := filepath.Join(root, "portable.age")
	exported := outputData(t, source, []string{
		"profile", "export", "--profile", "portable", "--output", archive, "--passphrase-file", passphraseFile,
	})
	if exported["changed"] != true {
		t.Fatalf("passphrase-file export failed: %#v", exported)
	}

	base := []string{"profile", "import", "--file", archive, "--passphrase-stdin"}
	code, out, diagnostic := invoke(t, target, passphrase+"\n", base...)
	if code != 0 || diagnostic != "" || strings.Contains(out+diagnostic, passphrase) {
		t.Fatalf("passphrase-stdin preview failed/leaked: code=%d out=%s err=%s", code, out, diagnostic)
	}
	var previewEnvelope struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &previewEnvelope); err != nil || !previewEnvelope.OK {
		t.Fatalf("invalid preview envelope: %s", out)
	}
	preview := previewEnvelope.Data
	apply := append(append([]string{}, base...), "--apply", preview["digest"].(string), "--request-id", tasks.ID())
	code, out, diagnostic = invoke(t, target, passphrase+"\n", apply...)
	if code != 0 || diagnostic != "" || strings.Contains(out+diagnostic, passphrase) {
		t.Fatalf("passphrase-stdin apply failed/leaked: code=%d out=%s err=%s", code, out, diagnostic)
	}
	value := outputData(t, target, []string{"var", "get", "MODE", "--profile", "portable"})
	if value["value"] != "noninteractive" {
		t.Fatalf("non-interactive round trip mismatch: %#v", value)
	}

	profiles, pathErr := target.dataDirectory()
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(profiles), "profile-transfer", "identity.txt")); !os.IsNotExist(err) {
		t.Fatalf("non-interactive passphrase import created recipient identity: %v", err)
	}
}

func TestProfilePassphraseOptionsRejectRecipientAndIdentityMixing(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	app := testApp(t)
	outputData(t, app, []string{"var", "set", "MODE", "--profile", "portable", "--value", "source"})

	passphraseFile := filepath.Join(root, "passphrase.txt")
	if err := os.WriteFile(passphraseFile, []byte("secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	prepared := outputData(t, app, []string{"profile", "transfer", "prepare"})
	recipient := prepared["item"].(map[string]any)["recipient"].(string)
	archive := filepath.Join(root, "portable.age")

	code, out, diagnostic := invoke(t, app, "", "profile", "export", "--profile", "portable", "--output", archive, "--recipient", recipient, "--passphrase-file", passphraseFile)
	if code != 2 || out != "" || !strings.Contains(diagnostic, "\"code\":\"invalid_argument\"") {
		t.Fatalf("recipient/passphrase mix accepted: code=%d out=%s err=%s", code, out, diagnostic)
	}

	outputData(t, app, []string{"profile", "export", "--profile", "portable", "--output", archive, "--passphrase-file", passphraseFile})
	identityPath := filepath.Join(root, "external-identity.txt")
	recipientPath := filepath.Join(root, "external-recipient.txt")
	if code, _, diagnostic := invoke(t, app, "", "backup", "keygen", "--identity-file", identityPath, "--recipient-file", recipientPath); code != 0 {
		t.Fatal(diagnostic)
	}
	code, out, diagnostic = invoke(t, app, "secret\n", "profile", "import", "--file", archive, "--identity-file", identityPath, "--passphrase-stdin")
	if code != 2 || out != "" || !strings.Contains(diagnostic, "\"code\":\"invalid_argument\"") {
		t.Fatalf("identity/passphrase mix accepted: code=%d out=%s err=%s", code, out, diagnostic)
	}
}

func TestNewUsesTerminalPassphrasePrompt(t *testing.T) {
	app := New("test", "test")
	if app.passphrasePrompt == nil {
		t.Fatal("default app did not configure terminal passphrase prompt")
	}
	_, err := app.passphrasePrompt(context.Background(), IO{In: strings.NewReader("")}, "prompt")
	if err == nil || err.Code != "passphrase_required" {
		t.Fatalf("non-terminal default prompt result: %+v", err)
	}
}
