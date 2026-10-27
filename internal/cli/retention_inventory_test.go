package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/tasks"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestCleanupRetentionUsesPassiveAuthenticationAndStrictSchema(t *testing.T) {
	app := testApp(t)
	directory, _ := app.dataDirectory()
	root := filepath.Dir(directory)
	app.userPaths = func() (paths.Directories, error) {
		return paths.Directories{Data: root, Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache"), Runtime: filepath.Join(root, "runtime")}, nil
	}
	outputData(t, app, []string{"var", "set", "URL", "--profile", "app", "--value", "public", "--revision", editToken(t, app), "--request-id", tasks.ID()})
	preview := func(profile string) retention.Inventory {
		t.Helper()
		data := outputData(t, app, []string{"cleanup", "preview", "--profile", profile})
		schemaData := outputData(t, app, []string{"schema", "cleanup", "preview"})
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("urn:retention", schemaData["output_schema"]); err != nil {
			t.Fatal(err)
		}
		schema, err := compiler.Compile("urn:retention")
		if err != nil {
			t.Fatal(err)
		}
		if err = schema.Validate(data); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(data["retention"])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "synthetic-") {
			t.Fatal("inventory disclosed input")
		}
		var out retention.Inventory
		if err = json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	public := preview("app")
	if public.Verification != "verified" || len(public.Buckets) != 1 || public.Buckets[0].Kind != "value_receipt" || public.Buckets[0].Verification != "public_metadata" {
		t.Fatal(public)
	}
	initializeTestVault(t, app)
	if code, _, diagnostic := invoke(t, app, "synthetic-inventory-secret", "sec", "set", "TOKEN", "--stdin", "--profile", "app"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, `{"external_execution_id":"inventory-job","selection":{"keys":["TOKEN"]}}`, "values", "execution", "prepare", "--stdin", "--profile", "app", "--request-id", tasks.ID()); code != 0 {
		t.Fatal(diagnostic)
	}
	before := outputData(t, app, []string{"vault", "status"})
	protected := preview("app")
	if protected.Verification != "verified" {
		t.Fatal(protected)
	}
	kinds := map[string]bool{}
	for _, b := range protected.Buckets {
		if b.Files < 1 || b.Bytes < 1 || b.CleanupEligible {
			t.Fatal(b)
		}
		kinds[b.Kind] = true
	}
	for _, kind := range []string{"execution_record", "execution_payload", "execution_receipt", "value_receipt"} {
		if !kinds[kind] {
			t.Fatal("missing inventory kind", kind, protected)
		}
	}
	if after := outputData(t, app, []string{"vault", "status"}); !reflect.DeepEqual(before, after) {
		t.Fatal("inventory changed session/idle", before, after)
	}
	if other := preview("other"); other.Verification != "verified" || len(other.Buckets) != 0 {
		t.Fatal("profile filter", other)
	}
	outputData(t, app, []string{"vault", "lock"})
	locked := preview("app")
	if locked.Verification != "verification_pending" || len(locked.Buckets) != 0 {
		t.Fatal("partial locked inventory", locked)
	}
}
