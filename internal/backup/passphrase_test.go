package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/jinyongp/devtools/internal/archivekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestPassphraseArchiveCreateImportAndRecovery(t *testing.T) {
	root := privateTestRoot(t)
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	target := Engine{Data: filepath.Join(root, "target", "data"), Config: filepath.Join(root, "target", "config"), Cache: filepath.Join(root, "target", "cache")}
	const passphrase = "PASS-PHRASE-CANARY"

	sourceStore := values.ModernStore{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, err := sourceStore.Update(context.Background(), func(state *values.Editor) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "from-source")
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	if _, err := source.CreateWithPassphrase(context.Background(), CreateOptions{Profile: "portable", Output: archive, Passphrase: passphrase}); err != nil {
		t.Fatalf("passphrase create: %v", err)
	}
	mode, err := ArchiveEncryptionMode(archive)
	if err != nil || mode != archivekey.Passphrase {
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
	got, readErr := (values.ModernStore{Directory: filepath.Join(target.Data, "profiles"), Profile: "portable"}).Read(context.Background())
	if readErr != nil {
		t.Fatal(readErr)
	}
	env, envErr := got.Environment("")
	if envErr != nil || env["VALUE"] != "from-source" {
		t.Fatalf("imported value: %#v %v", env, envErr)
	}

	if _, err := (values.ModernStore{Directory: filepath.Join(target.Data, "profiles"), Profile: "portable"}).Update(context.Background(), func(state *values.Editor) (bool, *protocol.Error) {
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
	if err != nil || recoveryMode != archivekey.Passphrase {
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
	recoveredState, recoveredReadErr := (values.ModernStore{Directory: filepath.Join(target.Data, "profiles"), Profile: "recovered"}).Read(context.Background())
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

func TestLegacyX25519BackupCanBeRestored(t *testing.T) {
	root := privateTestRoot(t)
	engine := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	legacy, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(root, "legacy-identity")
	if err := fixtureWriteFile(identity, []byte(legacy.String()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := values.ModernStore{Directory: filepath.Join(engine.Data, "profiles"), Profile: "portable"}
	if _, err := store.Update(context.Background(), func(state *values.Editor) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "x25519")
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	// Use the age library's original X25519 recipient directly to build a
	// compatibility fixture independent of the current key generation path.
	if _, err := engine.createArchive(context.Background(), "portable", archive, func(snapshot Snapshot) ([]byte, error) {
		return encodeTo(snapshot, legacy.Recipient())
	}); err != nil {
		t.Fatal(err)
	}
	mode, err := ArchiveEncryptionMode(archive)
	if err != nil || mode != archivekey.X25519 {
		t.Fatalf("archive mode = %q, %v", mode, err)
	}
	ctx := context.Background()
	preview, failure := engine.Restore(ctx, archive, identity, "portable", "restored", "", "", false)
	if failure != nil || preview.Applied || preview.Digest == "" {
		t.Fatalf("restore preview: %#v %v", preview, failure)
	}
	result, failure := engine.Restore(ctx, archive, identity, "portable", "restored", preview.Digest, tasks.ID(), false)
	if failure != nil || !result.Applied {
		t.Fatalf("restore apply: %#v %v", result, failure)
	}
	store.Profile = "restored"
	state, readErr := store.Read(ctx)
	if readErr != nil {
		t.Fatal(readErr)
	}
	env, envErr := state.Environment("")
	if envErr != nil || env["VALUE"] != "x25519" {
		t.Fatalf("restored value: %#v %v", env, envErr)
	}
}

func TestPassphraseImportRejectsWrongPassphraseWithoutMutation(t *testing.T) {
	root := privateTestRoot(t)
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	target := Engine{Data: filepath.Join(root, "target", "data"), Config: filepath.Join(root, "target", "config"), Cache: filepath.Join(root, "target", "cache")}

	sourceStore := values.ModernStore{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, err := sourceStore.Update(context.Background(), func(state *values.Editor) (bool, *protocol.Error) {
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
