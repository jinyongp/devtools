package profilerename

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/testfs"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func fixtureStore(t *testing.T) Store {
	t.Helper()
	path := filepath.Join(testfs.TempDir(t), "data")
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	return Store{Data: path}
}

func storeFiles(t *testing.T, root string) map[string][sha256.Size]byte {
	t.Helper()
	files := map[string][sha256.Size]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = sha256.Sum256(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRenamePreservesSealedProfileJournalPortsAndReceipt(t *testing.T) {
	ctx := context.Background()
	s := fixtureStore(t)
	master := vault.NewMaterial(bytes.Repeat([]byte{0x36}, 32))
	defer master.Destroy()
	key := vault.NewMaterial(bytes.Repeat([]byte{0x71}, 32))
	defer key.Destroy()
	vaultID, err := vault.NewID()
	if err != nil {
		t.Fatal(err)
	}
	store := vault.FileStore{Root: s.Data, VaultID: vaultID, CatalogKey: master}
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	profile, err := vault.NewSealedProfile("old", vaultID, master, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Set("TOKEN", vault.Secret, "", "fixture-value", key); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, profile, key); err != nil {
		t.Fatal(err)
	}

	taskRequest := tasks.Request{Action: "task.add", Body: tasks.Object{"title": "Keep this task"}, Options: map[string]string{"request-id": tasks.ID(), "profile": "old"}}
	oldTasks := s.renameTaskStore("old")
	added, failure := oldTasks.Execute(ctx, taskRequest)
	if failure != nil {
		t.Fatal(failure)
	}
	portStore := ports.ForDataRoot(s.Data)
	portStore.Probe = func(int) (bool, *protocol.Error) { return true, nil }
	var original ports.Assignment
	if failure := portStore.Update(ctx, func(state *ports.State) (bool, *protocol.Error) {
		instance, failure := state.Register("old", filepath.Join(s.Data, "project"))
		if failure != nil {
			return false, failure
		}
		alias := "local"
		instance.Alias = &alias
		port := 31416
		original, _, failure = portStore.Allocate(ctx, state, *instance, "web", project.Port{Port: &port, Strict: true}, []int{port, port})
		if failure != nil {
			return false, failure
		}
		ref := network.RouteRef{Scope: "project", Profile: "old", InstanceID: instance.ID, Proxy: "web"}
		return true, state.SetActivation(network.Activation{Mode: "disabled", Route: ref})
	}); failure != nil {
		t.Fatal(failure)
	}
	legacyPorts := portStore.Directory
	unclaim, failure := portStore.Claim(ctx, original.ID, []string{"web"})
	if failure != nil {
		t.Fatal(failure)
	}
	_, blocked := s.Preview(ctx, "old", "new")
	unclaim()
	if blocked == nil || blocked.Code != "port_run_active" {
		t.Fatalf("preview accepted a claimed port: %v", blocked)
	}
	plan, failure := s.Preview(ctx, "old", "new")
	if failure != nil || !plan.Values || !plan.Tasks || !plan.Ports {
		t.Fatalf("preview = %+v, %v", plan, failure)
	}
	requestID := tasks.ID()
	if _, _, failure := s.Apply(ctx, "old", "new", plan.Digest, requestID, vault.FileStore{}); failure == nil || failure.Code != "vault_locked" {
		t.Fatalf("protected rename accepted without key: %v", failure)
	}
	got, replay, failure := s.Apply(ctx, "old", "new", plan.Digest, requestID, store)
	if failure != nil || replay || got != plan {
		t.Fatalf("apply = %+v, %v, %v", got, replay, failure)
	}
	portStore = ports.ForDataRoot(s.Data)
	if _, failure := s.Preview(ctx, "old", "other"); failure == nil || failure.Code != "profile_renamed" {
		t.Fatalf("old address was usable as source: %v", failure)
	}
	release, err := maintenance.AcquirePassiveQuery(ctx, s.Data)
	if err != nil {
		t.Fatal(err)
	}
	moved, movedKey, readErr := store.ReadAnyWithMasterHeld(ctx, "new", master)
	_, _, oldErr := store.ReadAnyWithMasterHeld(ctx, "old", master)
	release()
	if readErr != nil || oldErr == nil || oldErr.Error() != "profile_not_found" {
		t.Fatalf("authenticated addresses: new=%v old=%v", readErr, oldErr)
	}
	defer movedKey.Destroy()
	if moved.ID != profile.ID || moved.Generation != profile.Generation || moved.Revision != profile.Revision || moved.KeyEpoch != profile.KeyEpoch ||
		!bytes.Equal(moved.WrappedKey, profile.WrappedKey) || !reflect.DeepEqual(moved.Values, profile.Values) {
		t.Fatal("renamed profile changed identity or ciphertext")
	}
	environment, _, err := moved.Environment("", movedKey, time.Now())
	if err != nil || environment["TOKEN"] != "fixture-value" {
		t.Fatal("could not read the original secret under the new name", err)
	}
	newTasks := s.renameTaskStore("new")
	taskRequest.Options["profile"] = "new"
	replayedTask, failure := newTasks.Execute(ctx, taskRequest)
	originalItem, _ := json.Marshal(added["item"])
	replayedItem, _ := json.Marshal(replayedTask["item"])
	if failure != nil || replayedTask["replayed"] != true || !bytes.Equal(replayedItem, originalItem) {
		t.Fatalf("task replay = %+v, %v", replayedTask, failure)
	}
	state, failure := newTasks.ReadCurrent(ctx)
	if failure != nil || fmt.Sprint(state.Revision) != fmt.Sprint(added["revision"]) || len(state.Items) != 1 {
		t.Fatalf("task history changed: %v", failure)
	}
	taskRequest.Body = tasks.Object{"title": "A different operation"}
	if _, failure := newTasks.Execute(ctx, taskRequest); failure == nil || failure.Code != "request_conflict" {
		t.Fatalf("historical task request accepted changed input: %v", failure)
	}
	taskRequest.Options["request-id"] = tasks.ID()
	taskRequest.Options["profile"] = "old"
	if _, failure := oldTasks.Execute(ctx, taskRequest); failure == nil || failure.Code != "profile_renamed" {
		t.Fatalf("task creation reused the retired name: %v", failure)
	}
	_, failure = (values.ModernStore{Directory: filepath.Join(s.Data, "profiles"), Profile: "old"}).Read(ctx)
	if failure == nil || failure.Code != "profile_renamed" {
		t.Fatalf("value lookup used the retired name: %v", failure)
	}
	if failure := portStore.Update(ctx, func(state *ports.State) (bool, *protocol.Error) {
		_, failure := state.Register("old", filepath.Join(s.Data, "new-project"))
		return true, failure
	}); failure == nil || failure.Code != "profile_renamed" {
		t.Fatalf("instance registration reused the retired name: %v", failure)
	}
	registry, failure := portStore.ReadContext(ctx)
	if failure != nil {
		t.Fatal(failure)
	}
	assignment := registry.Get(original.ID, "web")
	ref := network.RouteRef{Scope: "project", Profile: "new", InstanceID: original.ID, Proxy: "web"}
	activation := registry.Activation(ref)
	if assignment == nil || assignment.Profile != "new" || assignment.Port != original.Port || !reflect.DeepEqual(assignment.Alias, original.Alias) || assignment.Directory != original.Directory || activation == nil || activation.Mode != "disabled" {
		t.Fatalf("instance, assignment or disabled route changed: %+v %+v", assignment, activation)
	}
	if sentinel, err := maintenance.Read(legacyPorts, 4096); err != nil || string(sentinel) != maintenance.RuntimeFenceDocument {
		t.Fatal("port rename reopened the old registry path", err)
	}
	got, replay, failure = s.Apply(ctx, "old", "new", plan.Digest, requestID, store)
	if failure != nil || !replay || got != plan {
		t.Fatalf("receipt retry = %+v, %v, %v", got, replay, failure)
	}
	if _, _, failure := s.Apply(ctx, "old", "other", plan.Digest, requestID, store); failure == nil || failure.Code != "request_conflict" {
		t.Fatalf("request UUID allowed a different target: %v", failure)
	}
	reverse, failure := s.Preview(ctx, "new", "old")
	if failure != nil {
		t.Fatal(failure)
	}
	if _, _, failure := s.Apply(ctx, "new", "old", reverse.Digest, tasks.ID(), store); failure != nil {
		t.Fatal(failure)
	}
	got, replay, failure = s.Apply(ctx, "old", "new", plan.Digest, requestID, store)
	if failure != nil || !replay || got != plan {
		t.Fatalf("receipt retry after reverse = %+v, %v, %v", got, replay, failure)
	}
}

func TestRenameTaskOnlyRejectsChangedInputAndForeignReservation(t *testing.T) {
	ctx := context.Background()
	s := fixtureStore(t)
	add := func(profile string) {
		t.Helper()
		if _, failure := s.renameTaskStore(profile).Execute(ctx, tasks.Request{Action: "task.add", Body: tasks.Object{"title": "Task"}, Options: map[string]string{"request-id": tasks.ID()}}); failure != nil {
			t.Fatal(failure)
		}
	}
	add("first")
	before := storeFiles(t, s.Data)
	stale, failure := s.Preview(ctx, "first", "second")
	if failure != nil {
		t.Fatal(failure)
	}
	if after := storeFiles(t, s.Data); !reflect.DeepEqual(after, before) {
		t.Fatal("preview changed the task-only store")
	}
	add("first")
	requestID := tasks.ID()
	if _, _, failure := s.Apply(ctx, "first", "second", stale.Digest, requestID, vault.FileStore{}); failure == nil || failure.Code != "input_changed" {
		t.Fatalf("stale preview accepted: %v", failure)
	}
	if _, err := os.Stat(filepath.Join(s.Data, profilekey.RenameNamesPath)); !os.IsNotExist(err) {
		t.Fatalf("stale preview published a reservation: %v", err)
	}
	fresh, failure := s.Preview(ctx, "first", "second")
	if failure != nil || fresh.Values || fresh.Ports || !fresh.Tasks {
		t.Fatalf("task-only preview = %+v, %v", fresh, failure)
	}
	if _, _, failure := s.Apply(ctx, "first", "second", fresh.Digest, requestID, vault.FileStore{}); failure != nil {
		t.Fatal(failure)
	}
	add("unrelated")
	if _, failure := s.Preview(ctx, "unrelated", "first"); failure == nil || failure.Code != "profile_name_reserved" {
		t.Fatalf("another profile could claim the old name: %v", failure)
	}
	next, failure := s.Preview(ctx, "second", "third")
	if failure != nil {
		t.Fatal(failure)
	}
	if _, _, failure := s.Apply(ctx, "second", "third", next.Digest, tasks.ID(), vault.FileStore{}); failure != nil {
		t.Fatal(failure)
	}
	body, err := os.ReadFile(filepath.Join(s.Data, profilekey.RenameNamesPath))
	if err != nil {
		t.Fatal(err)
	}
	var names profilekey.RenameNames
	if err := json.Unmarshal(body, &names); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"first", "second", "third"} {
		if names.Owners[alias] != names.Owners["third"] || names.Current[names.Owners[alias]] != "third" {
			t.Fatalf("name %s lost its current owner: %+v", alias, names)
		}
	}
}

func TestPublicValueReceiptReplaysAfterRename(t *testing.T) {
	ctx := context.Background()
	s := fixtureStore(t)
	original := values.ModernStore{Directory: filepath.Join(s.Data, "profiles"), Profile: "original"}
	view, failure := original.Inspect(ctx, "")
	if failure != nil {
		t.Fatal(failure)
	}
	value := "stable"
	change := values.Change{Action: "variable.set", Key: "MODE", Value: &value, Revision: view.Revision, RequestID: tasks.ID()}
	first, failure := original.Apply(ctx, change)
	if failure != nil {
		t.Fatal(failure)
	}
	plan, failure := s.Preview(ctx, "original", "current")
	if failure != nil {
		t.Fatal(failure)
	}
	if _, _, failure := s.Apply(ctx, plan.Source, plan.Target, plan.Digest, tasks.ID(), vault.FileStore{}); failure != nil {
		t.Fatal(failure)
	}
	current := original
	current.Profile = "current"
	replay, failure := current.Apply(ctx, change)
	if failure != nil || !replay.Replayed || replay.Profile != first.Profile || replay.Revision != first.Revision {
		t.Fatalf("original result not replayed: %+v %v", replay, failure)
	}
	value = "different"
	if _, failure := current.Apply(ctx, change); failure == nil || failure.Code != "request_conflict" {
		t.Fatalf("saved receipt accepted different input: %v", failure)
	}
	change.RequestID = tasks.ID()
	if _, failure := original.Apply(ctx, change); failure == nil || failure.Code != "profile_renamed" {
		t.Fatalf("value writer recreated the old profile: %v", failure)
	}
}

func TestRenameResumesPendingTransitionWithOriginalRequest(t *testing.T) {
	ctx := context.Background()
	s := fixtureStore(t)
	request := tasks.Request{Action: "task.add", Body: tasks.Object{"title": "Survives transition"}, Options: map[string]string{"request-id": tasks.ID()}}
	created, condition := s.renameTaskStore("old").Execute(ctx, request)
	if condition != nil {
		t.Fatal(condition)
	}
	plan, condition := s.Preview(ctx, "old", "new")
	if condition != nil {
		t.Fatal(condition)
	}
	id := tasks.ID()
	intent, err := json.Marshal(transitionRequest{Source: "old", Target: "new", Digest: plan.Digest, RequestID: id})
	if err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireRuntimeTransition(ctx, s.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.Write(filepath.Join(s.Data, maintenance.VersionedRuntime, "rename-request.json"), intent); err != nil {
		release()
		t.Fatal(err)
	}
	err = maintenance.StageRuntimeTransitionHeld(ctx, s.Data)
	release()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, condition := s.Apply(ctx, "old", "new", plan.Digest, tasks.ID(), vault.FileStore{}); condition == nil || condition.Code != "storage_transition_pending" {
		t.Fatalf("different request could resume a transition: %v", condition)
	}
	lock := filepath.Join(s.Data, maintenance.VersionedRuntime, "network-operations", "request.lock")
	unlock, err := maintenance.LockExclusive(ctx, lock)
	if err != nil {
		t.Fatal(err)
	}
	_, _, condition = s.Apply(ctx, "old", "new", plan.Digest, id, vault.FileStore{})
	unlock()
	if condition == nil || condition.Code != "runtime_writer_active" {
		t.Fatalf("apply accepted an active writer in staged storage: %v", condition)
	}
	if result, replay, condition := s.Apply(ctx, "old", "new", plan.Digest, id, vault.FileStore{}); condition != nil || replay || result != plan {
		t.Fatalf("retry after writer finished: %+v %v %v", result, replay, condition)
	}
	result, condition := s.renameTaskStore("new").Execute(ctx, request)
	if condition != nil || result["replayed"] != true || fmt.Sprint(result["affected_ids"]) != fmt.Sprint(created["affected_ids"]) {
		t.Fatalf("original task receipt did not survive: %+v %v", result, condition)
	}
}
