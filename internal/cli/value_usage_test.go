package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValueUsageCLIExposesMetadataAndPreservesSessionIdle(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	root := privateTempDir(t)
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile = \"app\"\n[commands.web]\nexec = [\"fixture\"]\ninject = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, err := invoke(t, app, "VALUE-USAGE-SECRET-CANARY", "sec", "set", "TOKEN", "--profile", "app", "--stdin"); code != 0 {
		t.Fatal(err)
	}
	if code, _, err := invoke(t, app, "", "var", "set", "URL", "--profile", "app", "--value", "VALUE-USAGE-PUBLIC-CANARY"); code != 0 {
		t.Fatal(err)
	}
	before := outputData(t, app, []string{"vault", "status"})
	for _, args := range [][]string{{"values", "inspect", "--profile", "app"}, {"values", "uses", "--dir", root}, {"command", "inspect", "web", "--dir", root}} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 0 || diagnostic != "" {
			t.Fatal("usage query", code, diagnostic)
		}
		for _, forbidden := range []string{"VALUE-USAGE-SECRET-CANARY", "VALUE-USAGE-PUBLIC-CANARY", "session_id", "challenge", "ciphertext", "launch_id"} {
			if strings.Contains(out, forbidden) {
				t.Fatal("usage exposed values or authority", forbidden)
			}
		}
		if !strings.Contains(out, "value_version") || !strings.Contains(out, "storage_generation") || !strings.Contains(out, "\"integrity_verified\":true") {
			t.Fatal("verified input metadata absent")
		}
	}
	after := outputData(t, app, []string{"vault", "status"})
	if mustJSON(t, before) != mustJSON(t, after) {
		t.Fatal("usage reads changed session lifetime")
	}
	for _, command := range []string{"inspect", "uses"} {
		schema := outputData(t, app, []string{"schema", "values", command})
		if schema["output_schema"] == nil {
			t.Fatal("missing usage schema")
		}
	}
	if code, _, err := invoke(t, app, "", "vault", "lock"); code != 0 {
		t.Fatal(err)
	}
	data := outputData(t, app, []string{"values", "inspect", "--profile", "app"})
	status := data["values"].(map[string]any)
	if status["state"] != "verification_pending" || status["current"] != nil {
		t.Fatal("locked query claimed verified input", status)
	}
	none := outputData(t, app, []string{"values", "inspect", "--profile", "app", "--bundle", "none"})
	if none["values"].(map[string]any)["state"] != "verification_pending" {
		t.Fatal("empty selection bypassed protected-root verification")
	}
}
