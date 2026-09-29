package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestPassphraseArchiveCreateImportAndRecovery(t *testing.T) {
	root := t.TempDir()
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	target := Engine{Data: filepath.Join(root, "target", "data"), Config: filepath.Join(root, "target", "config"), Cache: filepath.Join(root, "target", "cache")}
	const passphrase = "PASS-PHRASE-CANARY"

	sourceStore := values.Store{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, err := sourceStore.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "from-source")
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	if _, err := source.CreateWithPassphrase(context.Background(), CreateOptions{Profile: "portable", Output: archive, Passphrase: passphrase}); err != nil {
		t.Fatalf("passphrase create: %v", err)
	}
	mode, err := ArchiveEncryptionMode(archive)
	if err != nil || mode != ArchiveEncryptionPassphrase {
		t.Fatalf("archive mode = %q, %v", mode, err)
	}

	preview, importErr := target.Import(context.Background(), ImportRequest{Path: archive, Passphrase: passphrase})
	if importErr != nil || preview.Plan.Applied || preview.Plan.Digest == "" {
		t.Fatalf("preview: %#v %v", preview.Plan, importErr)
	}
	apply := ImportRequest{Path: archive, Passphrase: passphrase, Expected: preview.Plan.Digest, RequestID: tasks.ID()}
	imported, importErr := target.Import(context.Background(), apply)
	if importErr != nil || !imported.Plan.Applied {
		t.Fatalf("apply: %#v %v", imported.Plan, importErr)
	}
	got, readErr := (values.Store{Directory: filepath.Join(target.Data, "profiles"), Profile: "portable"}).Read(context.Background())
	if readErr != nil {
		t.Fatal(readErr)
	}
	env, envErr := got.Environment("")
	if envErr != nil || env["VALUE"] != "from-source" {
		t.Fatalf("imported value: %#v %v", env, envErr)
	}

	if _, err := (values.Store{Directory: filepath.Join(target.Data, "profiles"), Profile: "portable"}).Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "before-replace")
	}); err != nil {
		t.Fatal(err)
	}
	fresh, importErr := target.Import(context.Background(), ImportRequest{Path: archive, Passphrase: passphrase})
	if importErr != nil {
		t.Fatal(importErr)
	}
	recoveryDir := filepath.Join(target.Data, "profile-transfer", "recovery")
	replaced, importErr := target.Import(context.Background(), ImportRequest{
		Path: archive, Passphrase: passphrase, Expected: fresh.Plan.Digest, RequestID: tasks.ID(), Replace: true, RecoveryDirectory: recoveryDir,
	})
	if importErr != nil || replaced.Plan.SafetyBackup == "" {
		t.Fatalf("replace: %#v %v", replaced.Plan, importErr)
	}
	recoveryMode, err := ArchiveEncryptionMode(replaced.Plan.SafetyBackup)
	if err != nil || recoveryMode != ArchiveEncryptionPassphrase {
		t.Fatalf("recovery mode = %q, %v", recoveryMode, err)
	}
	recoveryPreview, importErr := target.Import(context.Background(), ImportRequest{Path: replaced.Plan.SafetyBackup, Passphrase: passphrase, Target: "recovered"})
	if importErr != nil {
		t.Fatal(importErr)
	}
	recovered, importErr := target.Import(context.Background(), ImportRequest{Path: replaced.Plan.SafetyBackup, Passphrase: passphrase, Target: "recovered", Expected: recoveryPreview.Plan.Digest, RequestID: tasks.ID()})
	if importErr != nil || !recovered.Plan.Applied {
		t.Fatalf("recovery apply: %#v %v", recovered.Plan, importErr)
	}
	recoveredState, recoveredReadErr := (values.Store{Directory: filepath.Join(target.Data, "profiles"), Profile: "recovered"}).Read(context.Background())
	if recoveredReadErr != nil {
		t.Fatal(recoveredReadErr)
	}
	recoveredEnv, recoveredEnvErr := recoveredState.Environment("")
	if recoveredEnvErr != nil || recoveredEnv["VALUE"] != "before-replace" {
		t.Fatalf("recovery value: %#v %v", recoveredEnv, recoveredEnvErr)
	}

	if body, err := os.ReadFile(archive); err != nil || string(body) == passphrase {
		t.Fatal("passphrase archive leaked passphrase material")
	}
}

func TestArchiveEncryptionModeRecognizesX25519(t *testing.T) {
	root := t.TempDir()
	engine := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipient); err != nil {
		t.Fatal(err)
	}
	store := values.Store{Directory: filepath.Join(engine.Data, "profiles"), Profile: "portable"}
	if _, err := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "x25519")
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	if _, err := engine.Create(context.Background(), "portable", archive, recipient); err != nil {
		t.Fatal(err)
	}
	mode, err := ArchiveEncryptionMode(archive)
	if err != nil || mode != ArchiveEncryptionRecipient {
		t.Fatalf("archive mode = %q, %v", mode, err)
	}
}

func TestPassphraseImportRejectsWrongPassphraseWithoutMutation(t *testing.T) {
	root := t.TempDir()
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	target := Engine{Data: filepath.Join(root, "target", "data"), Config: filepath.Join(root, "target", "config"), Cache: filepath.Join(root, "target", "cache")}

	sourceStore := values.Store{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, err := sourceStore.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "source")
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	if _, err := source.CreateWithPassphrase(context.Background(), CreateOptions{Profile: "portable", Output: archive, Passphrase: "correct-passphrase"}); err != nil {
		t.Fatal(err)
	}

	if _, err := target.Import(context.Background(), ImportRequest{Path: archive, Passphrase: "wrong-passphrase"}); err == nil || err.Code != "invalid_backup" {
		t.Fatalf("wrong passphrase result: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(target.Data, "profiles")); !os.IsNotExist(err) {
		t.Fatalf("wrong passphrase mutated target data: %v", err)
	}
}
