package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsReportIsReadOnlyMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	app := testApp(t)
	if code, _, diagnostic := invoke(t, app, "DIAGNOSTICS-CANARY", "sec", "set", "TOKEN", "--profile", "app", "--stdin"); code != 0 {
		t.Fatal(code, diagnostic)
	}
	code, out, diagnostic := invoke(t, app, "", "diagnostics")
	if code != 0 || diagnostic != "" {
		t.Fatalf("diagnostics: %d %s %s", code, out, diagnostic)
	}
	for _, forbidden := range []string{"DIAGNOSTICS-CANARY", "\"content\"", "\"token\"", "\"context\"", "\"recipient\""} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("diagnostics leaked forbidden material %q: %s", forbidden, out)
		}
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err)
	}
	data := response["data"].(map[string]any)
	if data["ready"] != true {
		t.Fatalf("diagnostics not ready: %#v", data)
	}
	profiles := data["profiles"].(map[string]any)
	items := profiles["items"].([]any)
	if profiles["status"] != "pass" || len(items) != 1 {
		t.Fatalf("unexpected profiles: %#v", profiles)
	}
	profile := items[0].(map[string]any)
	if profile["profile"] != "app" || profile["values"] != true {
		t.Fatalf("profile metadata missing: %#v", profile)
	}
	dashboard := data["dashboard"].(map[string]any)
	backup := data["backup"].(map[string]any)
	if dashboard["status"] != "pass" || dashboard["running"] != false || backup["status"] != "pass" || backup["configured"] != false {
		t.Fatalf("normal stopped/unconfigured state reported as failure: dashboard=%#v backup=%#v", dashboard, backup)
	}
}

func TestDiagnosticsSchemaExcludesSensitivePayloads(t *testing.T) {
	app := testApp(t)
	data := outputData(t, app, []string{"schema", "diagnostics"})
	schema := data["output_schema"].(map[string]any)
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"content", "token", "context", "recipient", "secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics schema exposes forbidden concept %q: %s", forbidden, text)
		}
	}
}
