package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProfileTransferFailureRemedies(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	source, target := testApp(t), testApp(t)
	outputData(t, source, []string{"var", "set", "MODE", "--profile", "portable", "--value", "source"})
	prepared := outputData(t, target, []string{"profile", "transfer", "prepare"})
	recipient := prepared["item"].(map[string]any)["recipient"].(string)
	archive := filepath.Join(root, "portable.age")
	outputData(t, source, []string{"profile", "export", "--profile", "portable", "--output", archive, "--recipient", recipient})
	base := []string{"profile", "import", "--file", archive}
	preview := outputData(t, target, base)
	outputData(t, target, append(append([]string{}, base...), "--apply", preview["digest"].(string), "--request-id", tasks.ID()))
	existing := outputData(t, target, base)
	invalidArchive := filepath.Join(root, "invalid.age")
	if err := os.WriteFile(invalidArchive, []byte("not an encrypted archive"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		app  *App
		args []string
		code string
	}{
		{"recipient", source, []string{"profile", "export", "--profile", "portable", "--recipient", "invalid"}, "invalid_recipient"},
		{"archive", target, []string{"profile", "import", "--file", invalidArchive}, "invalid_backup"},
		{"target", target, append(append([]string{}, base...), "--apply", existing["digest"].(string), "--request-id", tasks.ID()), "profile_exists"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exit, out, diagnostic := invoke(t, tc.app, "", tc.args...)
			if exit != 3 || out != "" {
				t.Fatalf("unexpected result: exit=%d", exit)
			}
			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Details struct {
						Remedies []protocol.Remedy `json:"remedies"`
					} `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(diagnostic), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error.Code != tc.code || len(envelope.Error.Details.Remedies) == 0 {
				t.Fatalf("missing structured transfer remedy: code=%s remedies=%d", envelope.Error.Code, len(envelope.Error.Details.Remedies))
			}
			for _, remedy := range envelope.Error.Details.Remedies {
				if remedy.Argv == nil || remedy.RequiredInputs == nil || strings.TrimSpace(remedy.Message) == "" {
					t.Fatal("incomplete remedy contract")
				}
			}
			for _, forbidden := range []string{"Backup operation could not be completed", "backup configure", "AGE-SECRET-KEY-", "identity.txt"} {
				if strings.Contains(diagnostic, forbidden) {
					t.Fatalf("transfer error contains forbidden diagnostic category %q", forbidden)
				}
			}
		})
	}
}

func TestProfileTransferUnsafeParentReturnsIdentityError(t *testing.T) {
	root := privateTempDir(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	app := testApp(t)
	outputData(t, app, []string{"profile", "transfer", "prepare"})
	profiles, profileErr := app.dataDirectory()
	if profileErr != nil {
		t.Fatal(profileErr)
	}
	directory := filepath.Join(filepath.Dir(profiles), "profile-transfer")
	identity := filepath.Join(directory, "identity.txt")
	before, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	for _, args := range [][]string{
		{"profile", "transfer", "prepare"},
		{"profile", "import", "--file", filepath.Join(root, "unused.age")},
	} {
		exit, out, diagnostic := invoke(t, app, "", args...)
		if exit != 3 || out != "" || !strings.Contains(diagnostic, `"code":"transfer_identity_invalid"`) {
			t.Fatalf("unsafe parent did not return identity error: exit=%d", exit)
		}
		if strings.Contains(diagnostic, identity) || strings.Contains(diagnostic, "AGE-SECRET-KEY-") {
			t.Fatal("identity diagnostic disclosure")
		}
	}
	after, err := os.ReadFile(identity)
	if err != nil || string(after) != string(before) {
		t.Fatal("unsafe parent caused identity replacement")
	}
}
