package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
)

func TestStorageDoctorDoesNotResolveProjectOrPrepareOnInspect(t *testing.T) {
	app := testApp(t)
	t.Chdir(privateTempDir(t))
	if err := os.WriteFile("devtools.toml", []byte("invalid TOML ["), 0600); err != nil {
		t.Fatal(err)
	}
	directory, _ := app.dataDirectory()
	root := filepath.Dir(directory)
	code, out, diagnostic := invoke(t, app, "", "doctor", "--storage")
	if code != 0 || diagnostic != "" {
		t.Fatal(code, out, diagnostic)
	}
	var result struct {
		Data struct {
			Ready   bool
			Effects string
		}
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Data.Ready || result.Data.Effects != "none" {
		t.Fatal(out, err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".maintenance")); !os.IsNotExist(err) {
		t.Fatal("inspection prepared storage", err)
	}
	code, out, diagnostic = invoke(t, app, "", "doctor", "--storage", "--fix")
	if code != 0 || diagnostic != "" || !strings.Contains(out, `"ready":true`) {
		t.Fatal(code, out, diagnostic)
	}
	assertContract(t, "DoctorReport", outputData(t, app, []string{"doctor", "--storage"}))
}

func TestStorageDoctorReportsLegacyAndFutureHeadersWithoutValues(t *testing.T) {
	for _, test := range []struct{ body, state string }{
		{`{"version":1,"profile":"app","keys":{"TOKEN":"private-value-sentinel"}}`, "migration_required"},
		{`{"vault_format":999,"profile":"app","values":["private-value-sentinel"]}`, "unsupported"},
	} {
		t.Run(test.state, func(t *testing.T) {
			app := testApp(t)
			directory, _ := app.dataDirectory()
			root := filepath.Dir(directory)
			release, err := maintenance.AcquireExclusive(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			release()
			if err := maintenance.Write(filepath.Join(directory, "617070.json"), []byte(test.body)); err != nil {
				t.Fatal(err)
			}
			code, out, diagnostic := invoke(t, app, "", "doctor", "--storage", "--check")
			if code != 3 || out != "" || strings.Contains(diagnostic, "private-value-sentinel") || !strings.Contains(diagnostic, `"state":"`+test.state+`"`) {
				t.Fatal(code, out, diagnostic)
			}
			assertContract(t, "DoctorReport", outputData(t, app, []string{"doctor", "--storage"}))
		})
	}
}

func TestStorageDoctorOptionConflicts(t *testing.T) {
	app := testApp(t)
	for _, args := range [][]string{{"--fix"}, {"--storage", "--fix", "--check"}, {"--storage", "--profile", "app"}, {"--storage", "--project-only"}, {"--storage", "--dir", "."}, {"--storage", "--mode", "foreground"}, {"--storage", "web"}} {
		code, out, diagnostic := invoke(t, app, "", append([]string{"doctor"}, args...)...)
		if code != 2 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
			t.Fatal(args, code, out, diagnostic)
		}
	}
}
