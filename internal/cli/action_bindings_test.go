package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestMissingPrivateInputKeepsOriginalRequestWithoutExposingFile(t *testing.T) {
	app := testApp(t)
	id := "00000000-0000-4000-8000-000000000013"
	action := protocol.Remedy{Argv: []string{"devtools", "vault", "rewrap", "--from-backend", "native", "--backend", "passphrase", "--request-id", id, "--new-passphrase-file", "PRIVATE_FILE"}, Effect: "value_write", RequiredInputs: []string{"new-passphrase-file"}}
	bound := protocol.BindGuidance(action, protocol.InvocationContext{}, app.ResolveAction).(protocol.Remedy)
	if bound.State != "needs_input" || len(bound.Argv) != 0 || bound.Bindings["request-id"] != id || bound.Bindings["from-backend"] != "native" || bound.Bindings["backend"] != "passphrase" || len(bound.InputSteps) == 0 {
		t.Fatal(bound)
	}
	encoded, _ := json.Marshal(bound)
	if strings.Contains(string(encoded), "PRIVATE_FILE") {
		t.Fatal("private file leaked")
	}
}

func TestNativeRunAuthenticationRecoveryPublishesExecutableUnlock(t *testing.T) {
	app := testApp(t)
	root := privateTempDir(t)
	dir := filepath.Join(root, "vault")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// Routing metadata is sufficient to choose native recovery. No OS key store
	// or real credential is accessed by the failing value admission or renderer.
	id, err := vault.NewID()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(vault.KeyFile{Format: vault.StorageVersion, VaultID: id, Backend: "keychain", Wrapped: []byte{1}})
	if err := os.WriteFile(filepath.Join(dir, "key-slot.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{vault.ErrLocked, vault.ErrInteractionRequired, vault.ErrBackendLocked} {
		_, condition := values.CaptureVault(context.Background(), root, "example", "", true, time.Now(), func(context.Context) (*vault.Material, error) { return nil, cause })
		if condition == nil {
			t.Fatal("expected admission failure")
		}
		actions := protocol.BindGuidance(condition.Details["remedies"], protocol.InvocationContext{}, app.ResolveAction).([]protocol.Remedy)
		if len(actions) != 1 || actions[0].State != "ready" || strings.Join(actions[0].Argv, " ") != "devtools vault unlock --interactive" || app.ResolveAction(actions[0].Argv) != nil {
			t.Fatal(cause, actions)
		}
		if cause == vault.ErrInteractionRequired && !strings.Contains(condition.Message, "authentication UI") {
			t.Fatal(condition)
		}
	}
}
