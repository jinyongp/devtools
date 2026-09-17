package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

	request := ImportRequest{Path: archive, IdentityPath: identity, RequestID: tasks.ID()}
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
	restored, err := (values.Store{Directory: filepath.Join(destination.Data, "profiles"), Profile: "portable"}).Read()
	if err != nil {
		t.Fatal(err)
	}
	env, err := restored.Environment("")
	if err != nil || env["VALUE"] != "transferred" {
		t.Fatalf("imported profile mismatch: %#v %v", env, err)
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
