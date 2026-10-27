package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestLockedEarlyFailuresKeepBackendRecovery(t *testing.T) {
	app := testApp(t)
	initializeTestVault(t, app)
	outputData(t, app, []string{"env", "create", "local", "--profile", "app"})
	revision := editToken(t, app)
	file := filepath.Join(privateTempDir(t), "input")
	if err := os.WriteFile(file, []byte("TOKEN=synthetic-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outputData(t, app, []string{"vault", "lock"})
	before := outputData(t, app, []string{"vault", "status"})
	var cases [][]string
	for _, kind := range []string{"var", "sec"} {
		for _, source := range []string{"stdin", "file"} {
			for _, guarded := range []bool{false, true} {
				args := []string{kind, "set", "TOKEN", "--profile", "app", "--env", "local", "--" + source}
				if source == "file" {
					args = append(args, file)
				}
				if guarded {
					args = append(args, "--revision", revision, "--request-id", tasks.ID())
				}
				cases = append(cases, args)
			}
		}
	}
	cases = append(cases, []string{"import", "--file", file, "--dry-run", "--profile", "app", "--env", "local"}, []string{"values", "request", "show", tasks.ID(), "--profile", "app"})
	for _, args := range cases {
		reader := &observedSecretReader{}
		var out, diagnostic bytes.Buffer
		code := app.Run(context.Background(), args, IO{In: reader, Out: &out, Err: &diagnostic})
		var envelope struct {
			Error struct {
				Code    string
				Details struct {
					Retry    string
					Remedies []protocol.Remedy
				}
			}
		}
		if code != 3 || json.Unmarshal(diagnostic.Bytes(), &envelope) != nil || envelope.Error.Code != "vault_locked" {
			t.Fatalf("%v: %d %s", args, code, &diagnostic)
		}
		details := envelope.Error.Details
		if details.Retry != "after_dependency_change" || len(details.Remedies) != 1 {
			t.Fatalf("%v: %+v", args, details)
		}
		remedy := details.Remedies[0]
		if !strings.Contains(strings.Join(remedy.Argv, " "), "vault unlock --passphrase-stdin") || !reflect.DeepEqual(remedy.RequiredInputs, []string{"stdin"}) || (args[0] == "sec" && reader.read) {
			t.Fatalf("%v: %+v read=%v", args, remedy, reader.read)
		}
	}
	if after := outputData(t, app, []string{"vault", "status"}); !reflect.DeepEqual(before, after) {
		t.Fatal("failed operations changed locked session")
	}
	// Authentication consumes a separate input; the original mutation can then run.
	output := "synthetic-shared-command-test-unlock\n"
	if code, _, diagnostic := invoke(t, app, output, "vault", "unlock", "--passphrase-stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "synthetic-secret", "sec", "set", "TOKEN", "--stdin", "--profile", "app", "--env", "local"); code != 0 {
		t.Fatal(diagnostic)
	}
}
