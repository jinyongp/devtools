package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsReportIsReadOnlyMetadata(t *testing.T) {
	home := privateTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	app := testApp(t)
	initializeTestVault(t, app)
	if code, _, diagnostic := invoke(t, app, "DIAGNOSTICS-CANARY", "sec", "set", "TOKEN", "--profile", "app", "--stdin"); code != 0 {
		t.Fatal(code, diagnostic)
	}
	before := outputData(t, app, []string{"vault", "status"})
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
	after := outputData(t, app, []string{"vault", "status"})
	if mustJSON(t, before) != mustJSON(t, after) {
		t.Fatal("diagnostics touched key session")
	}
	if data["vault"].(map[string]any)["state"] != "unlocked" {
		t.Fatal("vault metadata missing")
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
	var inspect func(any)
	inspect = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			// A kind enum may say "secret"; a payload property must not expose it.
			if properties, ok := v["properties"].(map[string]any); ok {
				for _, forbidden := range []string{"content", "token", "context", "recipient", "secret"} {
					if _, present := properties[forbidden]; present {
						t.Fatalf("diagnostics schema exposes forbidden payload field %q", forbidden)
					}
				}
			}
			for _, child := range v {
				inspect(child)
			}
		case []any:
			for _, child := range v {
				inspect(child)
			}
		}
	}
	inspect(schema)
}

func TestDiagnosticsPlatformMatchesPublishedSchema(t *testing.T) {
	app := testApp(t)
	data := outputData(t, app, []string{"diagnostics"})
	platform := data["platform"].(map[string]any)
	schema := platformDiagnosticsSchema()
	properties := schema["properties"].(map[string]any)
	for _, name := range schema["required"].([]string) {
		if _, ok := platform[name]; !ok {
			t.Fatal("required platform field missing", name)
		}
	}
	for name, value := range platform {
		field, ok := properties[name].(map[string]any)
		if !ok {
			t.Fatal("undocumented platform field", name)
		}
		if options, ok := field["enum"].([]string); ok {
			found := false
			for _, option := range options {
				if value == option {
					found = true
				}
			}
			if !found {
				t.Fatal("platform enum mismatch", name, value)
			}
		}
	}
	if _, ok := platform["storage_supported"].(bool); !ok {
		t.Fatal("storage flag type")
	}
	if _, ok := platform["limitations"].([]any); !ok {
		t.Fatal("limits type")
	}
}
