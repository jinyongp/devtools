package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestEncryptedRestoreAndReplay(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipient); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Configure(filepath.Join(root, "backups"), recipient); err != nil {
		t.Fatal(err)
	}
	store := values.Store{Directory: filepath.Join(e.Data, "profiles"), Profile: "source"}
	if _, err := store.Update(context.Background(), func(s *values.State) (bool, *protocol.Error) {
		return s.Set(values.Secret, "TOKEN", "", "SECRET_CANARY")
	}); err != nil {
		t.Fatal(err)
	}
	ts := tasks.Store{Directory: filepath.Join(e.Data, "tasks"), Profile: "source"}
	added, err := ts.Execute(context.Background(), tasks.Request{Action: "task.add", Body: tasks.Object{"title": "test"}, Options: map[string]string{"request-id": tasks.ID()}})
	if err != nil {
		t.Fatal(err)
	}
	itemID := added["item"].(tasks.Object)["id"].(string)
	claim, err := ts.Execute(context.Background(), tasks.Request{Action: "run.claimed", Target: itemID, Options: map[string]string{"request-id": tasks.ID()}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.Create(context.Background(), "source", "", "")
	if err != nil {
		t.Fatal(err)
	}
	path := out.Path
	ciphertext, _ := os.ReadFile(path)
	if strings.Contains(string(ciphertext), "SECRET_CANARY") {
		t.Fatal("plaintext backup")
	}
	inspected, err := Inspect(path, identity)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(inspected)
	if strings.Contains(string(b), "SECRET_CANARY") {
		t.Fatal("metadata leak")
	}
	plan, err := e.Restore(context.Background(), path, identity, "source", "copy", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	id := tasks.ID()
	result, err := e.Restore(context.Background(), path, identity, "source", "copy", plan.Digest, id, false)
	if err != nil || !result.Applied || result.Replayed {
		t.Fatal(err)
	}
	again, err := e.Restore(context.Background(), path, identity, "source", "copy", plan.Digest, id, false)
	if err != nil || again.Digest != result.Digest || !again.Replayed {
		t.Fatal("retry failed", err)
	}
	restored, err := (values.Store{Directory: store.Directory, Profile: "copy"}).Read()
	if err != nil {
		t.Fatal(err)
	}
	env, err := restored.Environment("")
	if err != nil || env["TOKEN"] != "SECRET_CANARY" {
		t.Fatal("secret not restored")
	}
	taskCopy := tasks.Store{Directory: ts.Directory, Profile: "copy"}
	state, err := taskCopy.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Current(itemID) != nil {
		t.Fatal("restored active claim")
	}
	journal, _ := os.ReadFile(filepath.Join(ts.Directory, file("tasks", "copy")[6:]))
	if strings.Contains(string(journal), claim["context"].(string)) {
		t.Fatal("restored credential")
	}
	plan, err = e.Restore(context.Background(), path, identity, "source", "copy", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	result, err = e.Restore(context.Background(), path, identity, "source", "copy", plan.Digest, tasks.ID(), true)
	if err != nil || result.SafetyBackup == "" {
		t.Fatal("missing safety backup", err)
	}
	if _, err = Inspect(result.SafetyBackup, identity); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "wrong")
	if _, err = Keygen(other, other+".pub"); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(path, other); err == nil {
		t.Fatal("wrong identity accepted")
	}
	ciphertext[len(ciphertext)-1] ^= 1
	bad := filepath.Join(root, "bad.age")
	os.WriteFile(bad, ciphertext, 0600)
	if _, err = e.Restore(context.Background(), bad, identity, "source", "copy", "", "", true); err == nil {
		t.Fatal("tampered backup accepted")
	}
}

func TestConcurrentImportReplaysSameRequest(t *testing.T) {
	root := t.TempDir()
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	destination := Engine{Data: filepath.Join(root, "destination", "data"), Config: filepath.Join(root, "destination", "config"), Cache: filepath.Join(root, "destination", "cache")}
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipient); err != nil {
		t.Fatal(err)
	}
	store := values.Store{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, err := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "transferred")
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	if _, err := source.Create(context.Background(), "portable", archive, recipient); err != nil {
		t.Fatal(err)
	}

	preview, err := destination.Import(context.Background(), ImportRequest{Path: archive, IdentityPath: identity})
	if err != nil || preview.Plan.Digest == "" || preview.Plan.Applied || preview.Plan.Replayed {
		t.Fatalf("import preview failed: %#v %v", preview.Plan, err)
	}
	request := ImportRequest{Path: archive, IdentityPath: identity, Expected: preview.Plan.Digest, RequestID: tasks.ID()}
	start := make(chan struct{})
	type outcome struct {
		result ImportResult
		err    *protocol.Error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			result, err := destination.Import(context.Background(), request)
			results <- outcome{result: result, err: err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent import failed: %v %v", first.err, second.err)
	}
	if !first.result.Plan.Applied || !second.result.Plan.Applied || first.result.Plan.Replayed == second.result.Plan.Replayed {
		t.Fatalf("expected one apply and one replay: %#v %#v", first.result.Plan, second.result.Plan)
	}
	if first.result.Diff.Different != second.result.Diff.Different {
		t.Fatalf("replay returned a different preview diff: %#v %#v", first.result.Diff, second.result.Diff)
	}
	restored, err := (values.Store{Directory: filepath.Join(destination.Data, "profiles"), Profile: "portable"}).Read()
	if err != nil {
		t.Fatal(err)
	}
	env, err := restored.Environment("")
	if err != nil || env["VALUE"] != "transferred" {
		t.Fatalf("imported profile mismatch: %#v %v", env, err)
	}
}

func TestImportPreviewApplyAndStaleProtection(t *testing.T) {
	root := t.TempDir()
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	destination := Engine{Data: filepath.Join(root, "destination", "data"), Config: filepath.Join(root, "destination", "config"), Cache: filepath.Join(root, "destination", "cache")}
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipient); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Configure(filepath.Join(root, "safety"), recipient); err != nil {
		t.Fatal(err)
	}
	sourceStore := values.Store{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, err := sourceStore.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		changed, setErr := state.Set(values.Variable, "VALUE", "", "imported")
		if setErr != nil {
			return false, setErr
		}
		secretChanged, secretErr := state.Set(values.Secret, "TOKEN", "", "IMPORT_SECRET_CANARY")
		return changed || secretChanged, secretErr
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	if _, err := source.Create(context.Background(), "portable", archive, recipient); err != nil {
		t.Fatal(err)
	}
	targetStore := values.Store{Directory: filepath.Join(destination.Data, "profiles"), Profile: "portable"}
	if _, err := targetStore.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "before")
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := destination.Import(context.Background(), ImportRequest{Path: archive, IdentityPath: identity})
	if err != nil || preview.Plan.Digest == "" || preview.Plan.Applied || preview.Plan.Replayed || len(preview.Plan.Targets) != 1 || !preview.Plan.Targets[0].Exists || !preview.Diff.Different {
		t.Fatalf("unexpected import preview: %#v %#v %v", preview.Plan, preview.Diff, err)
	}
	previewJSON, _ := json.Marshal(preview)
	if strings.Contains(string(previewJSON), "IMPORT_SECRET_CANARY") {
		t.Fatal("import preview leaked secret material")
	}
	before, err := targetStore.Read()
	if err != nil {
		t.Fatal(err)
	}
	beforeEnv, err := before.Environment("")
	if err != nil || beforeEnv["VALUE"] != "before" {
		t.Fatalf("preview changed target: %#v %v", beforeEnv, err)
	}
	if _, err = destination.Import(context.Background(), ImportRequest{Path: archive, IdentityPath: identity, Expected: preview.Plan.Digest, RequestID: tasks.ID()}); err == nil || err.Code != "profile_exists" {
		t.Fatalf("existing target applied without replace: %v", err)
	}

	if _, err := targetStore.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "changed-after-preview")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = destination.Import(context.Background(), ImportRequest{Path: archive, IdentityPath: identity, Expected: preview.Plan.Digest, RequestID: tasks.ID(), Replace: true}); err == nil || err.Code != "revision_conflict" {
		t.Fatalf("stale import preview applied: %v", err)
	}

	fresh, err := destination.Import(context.Background(), ImportRequest{Path: archive, IdentityPath: identity})
	if err != nil || fresh.Plan.Digest == preview.Plan.Digest {
		t.Fatalf("target mutation did not change preview digest: %#v %#v %v", preview.Plan, fresh.Plan, err)
	}
	requestID := tasks.ID()
	apply := ImportRequest{Path: archive, IdentityPath: identity, Expected: fresh.Plan.Digest, RequestID: requestID, Replace: true}
	applied, err := destination.Import(context.Background(), apply)
	if err != nil || !applied.Plan.Applied || applied.Plan.Replayed || applied.Plan.SafetyBackup == "" {
		t.Fatalf("replacement import failed: %#v %v", applied.Plan, err)
	}
	if _, err := Inspect(applied.Plan.SafetyBackup, identity); err != nil {
		t.Fatalf("invalid safety backup: %v", err)
	}
	replayed, err := destination.Import(context.Background(), apply)
	if err != nil || !replayed.Plan.Applied || !replayed.Plan.Replayed || replayed.Plan.Digest != applied.Plan.Digest {
		t.Fatalf("import replay failed: %#v %v", replayed.Plan, err)
	}
	if !reflect.DeepEqual(replayed.Diff, applied.Diff) {
		t.Fatalf("replay changed import diff: %#v %#v", applied.Diff, replayed.Diff)
	}
	conflicting := apply
	conflicting.Target = "other"
	if _, err = destination.Import(context.Background(), conflicting); err == nil || err.Code != "request_conflict" {
		t.Fatalf("changed retry input did not conflict: %v", err)
	}
	restored, err := targetStore.Read()
	if err != nil {
		t.Fatal(err)
	}
	restoredEnv, err := restored.Environment("")
	if err != nil || restoredEnv["VALUE"] != "imported" || restoredEnv["TOKEN"] != "IMPORT_SECRET_CANARY" {
		t.Fatalf("replacement import mismatch: %#v %v", restoredEnv, err)
	}
}

func TestImportRejectsWrongIdentityAndTamperedArchive(t *testing.T) {
	root := t.TempDir()
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	destination := Engine{Data: filepath.Join(root, "destination", "data"), Config: filepath.Join(root, "destination", "config"), Cache: filepath.Join(root, "destination", "cache")}
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipient); err != nil {
		t.Fatal(err)
	}
	store := values.Store{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, err := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "portable")
	}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "portable.age")
	if _, err := source.Create(context.Background(), "portable", archive, recipient); err != nil {
		t.Fatal(err)
	}
	wrongIdentity, wrongRecipient := filepath.Join(root, "wrong-identity"), filepath.Join(root, "wrong-recipient")
	if _, err := Keygen(wrongIdentity, wrongRecipient); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Import(context.Background(), ImportRequest{Path: archive, IdentityPath: wrongIdentity}); err == nil || err.Code != "invalid_backup" {
		t.Fatalf("wrong identity accepted: %v", err)
	}
	ciphertext, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	tampered := filepath.Join(root, "tampered.age")
	if err := os.WriteFile(tampered, ciphertext, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Import(context.Background(), ImportRequest{Path: tampered, IdentityPath: identity}); err == nil || err.Code != "invalid_backup" {
		t.Fatalf("tampered archive accepted: %v", err)
	}
}

func TestStalePreviewPreservesTarget(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	key, pub := filepath.Join(root, "key"), filepath.Join(root, "pub")
	Keygen(key, pub)
	s := values.Store{Directory: filepath.Join(e.Data, "profiles"), Profile: "source"}
	if _, err := s.Update(context.Background(), func(s *values.State) (bool, *protocol.Error) { return s.Set(values.Variable, "A", "", "old") }); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "backup.age")
	if _, err := e.Create(context.Background(), "source", path, pub); err != nil {
		t.Fatal(err)
	}
	plan, err := e.Restore(context.Background(), path, key, "source", "target", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	target := values.Store{Directory: s.Directory, Profile: "target"}
	target.Update(context.Background(), func(s *values.State) (bool, *protocol.Error) { return s.Set(values.Variable, "A", "", "new") })
	if _, err = e.Restore(context.Background(), path, key, "source", "target", plan.Digest, tasks.ID(), false); err == nil || err.Code != "revision_conflict" {
		t.Fatal("stale preview applied", err)
	}
}

func TestBackupStatusAndDirectRecipient(t *testing.T) {
	root := t.TempDir()
	e := Engine{Data: filepath.Join(root, "data"), Config: filepath.Join(root, "config"), Cache: filepath.Join(root, "cache")}
	status, statusErr := e.Status()
	if statusErr != nil || status.Configured || status.Directory != "" || status.Recipient != "" {
		t.Fatalf("unexpected unconfigured status: %#v %v", status, statusErr)
	}

	identity, recipientPath := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, err := Keygen(identity, recipientPath); err != nil {
		t.Fatal(err)
	}
	recipientBytes, readErr := os.ReadFile(recipientPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	recipientValue := strings.TrimSpace(string(recipientBytes))
	store := values.Store{Directory: filepath.Join(e.Data, "profiles"), Profile: "portable"}
	if _, err := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Variable, "VALUE", "", "portable")
	}); err != nil {
		t.Fatal(err)
	}
	directArchive := filepath.Join(root, "direct.age")
	if _, err := e.CreateWithRecipient(context.Background(), CreateOptions{Profile: "portable", Output: directArchive, Recipient: recipientValue}); err != nil {
		t.Fatalf("direct recipient create failed: %v", err)
	}
	if _, err := Inspect(directArchive, identity); err != nil {
		t.Fatalf("direct recipient archive is invalid: %v", err)
	}
	if _, err := e.CreateWithRecipient(context.Background(), CreateOptions{Profile: "portable", Output: filepath.Join(root, "conflict.age"), RecipientPath: recipientPath, Recipient: recipientValue}); err == nil || err.Code != "invalid_argument" {
		t.Fatalf("recipient/file conflict was accepted: %v", err)
	}

	backupDirectory := filepath.Join(root, "backups")
	if _, err := e.Configure(backupDirectory, recipientPath); err != nil {
		t.Fatal(err)
	}
	status, statusErr = e.Status()
	if statusErr != nil || !status.Configured || status.Directory != backupDirectory || status.Recipient != recipientValue {
		t.Fatalf("unexpected configured status: %#v %v", status, statusErr)
	}
	configuredArchive, createErr := e.Create(context.Background(), "portable", "", "")
	if createErr != nil || configuredArchive.Path == "" {
		t.Fatalf("configured recipient fallback failed: %#v %v", configuredArchive, createErr)
	}
	if _, err := Inspect(configuredArchive.Path, identity); err != nil {
		t.Fatalf("configured fallback archive is invalid: %v", err)
	}
}

func TestErrorExitCodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "same-key-file")
	if _, err := Keygen(path, path); err == nil || err.Code != "invalid_argument" || err.ExitCode != 2 {
		t.Fatal("invalid backup input contract", err)
	}
	for code, want := range map[string]int{"invalid_argument": 2, "invalid_backup": 3, "storage_error": 1, "canceled": 130} {
		if err := failure(code); err.ExitCode != want {
			t.Errorf("%s exit code = %d, want %d", code, err.ExitCode, want)
		}
	}
}
