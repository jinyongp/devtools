package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

const snapshotCredential = "synthetic-frozen-credential-only"

type failedSnapshotOutput struct{}

func (failedSnapshotOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestStartedChildOutputFailureHasPartialEffects(t *testing.T) {
	command, dependencies, _ := executionSnapshotFixture(t)
	parent := []string{"BASE=before", "DEVTOOLS_EXEC_SNAPSHOT_CHILD=1"}
	prepared, e := PrepareInput(context.Background(), command, dependencies, parent, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Release()
	_, failure := prepared.Execute(context.Background(), parent, nil, nil, failedSnapshotOutput{}, io.Discard)
	if failure == nil || protocol.Public(failure, "command run").Details["effects"] != "partial" {
		t.Fatal("a spawned child was reported as no change", failure)
	}
}

func TestPreparedExecutableReplacementRequiresRefresh(t *testing.T) {
	command, dependencies, _ := executionSnapshotFixture(t)
	fixture := filepath.Join(command.Directory, "fixture-executable.exe")
	if err := os.WriteFile(fixture, []byte("synthetic executable before"), 0700); err != nil {
		t.Fatal(err)
	}
	command.Exec = []string{fixture}
	command.Inject = false
	prepared, e := PrepareInput(context.Background(), command, dependencies, os.Environ(), PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Release()
	encoded, e := prepared.Input.EncodeForLauncher()
	if e != nil {
		t.Fatal(e)
	}
	defer encoded.Destroy()
	if err := os.Remove(fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte("synthetic executable after"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, e := prepared.Input.Open(); e == nil || e.Code != "input_changed" {
		t.Fatal("replacement accepted", e)
	}
	transported, e := DecodeLauncherInput(encoded)
	if e != nil {
		t.Fatal(e)
	}
	defer transported.Destroy()
	if _, e := transported.Open(); e == nil || e.Code != "input_changed" {
		t.Fatal("transport accepted replacement", e)
	}
}

func TestFrozenInputChild(t *testing.T) {
	if os.Getenv("DEVTOOLS_EXEC_SNAPSHOT_CHILD") != "1" {
		return
	}
	if os.Getenv("TOKEN") != snapshotCredential || os.Getenv("BASE") != "before" || os.Getenv("DEVTOOLS_TASK_CONTEXT") != "" {
		os.Exit(42)
	}
	fmt.Print("snapshot-ok")
	os.Exit(0)
}
func executionSnapshotFixture(t *testing.T) (Command, Dependencies, values.Store) {
	t.Helper()
	root := privateTempDir(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store := values.Store{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	if _, e := store.Update(context.Background(), func(s *values.State) (bool, *protocol.Error) {
		return s.Set(values.Secret, "TOKEN", "", snapshotCredential)
	}); e != nil {
		t.Fatal(e)
	}
	return Command{Project: project.Context{Profile: "app", Root: root}, Directory: root, Exec: []string{exe, "-test.run=^TestFrozenInputChild$"}, Inject: true}, Dependencies{ValuesDirectory: store.Directory}, store
}
func TestActualInputSurvivesStoreAndCallerChanges(t *testing.T) {
	command, dependencies, store := executionSnapshotFixture(t)
	parent := []string{"BASE=before", "DEVTOOLS_EXEC_SNAPSHOT_CHILD=1", "DEVTOOLS_TASK_CONTEXT=synthetic-control-context"}
	plan, e := ResolvePlan(command, parent, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer plan.Destroy()
	snapshot, e := AuthenticateSnapshot(context.Background(), plan, dependencies)
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Destroy()
	if _, e := store.Update(context.Background(), func(s *values.State) (bool, *protocol.Error) {
		return s.Set(values.Secret, "TOKEN", "", "synthetic-later-credential")
	}); e != nil {
		t.Fatal(e)
	}
	parent[0] = "BASE=after"
	command.Exec[1] = "-test.run=missing"
	prepared, e := Materialize(context.Background(), snapshot, dependencies)
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Release()
	// Public compatibility fields and later parent arguments cannot change the
	// already encoded actual payload used by Execute.
	prepared.Args[1] = "-test.run=missing"
	prepared.Injected["TOKEN"] = "synthetic-overwrite"
	var out, diagnostic bytes.Buffer
	code, e := prepared.Execute(context.Background(), []string{"BASE=after"}, nil, nil, &out, &diagnostic)
	if e != nil || code != 0 || !strings.Contains(out.String(), "snapshot-ok") {
		t.Fatal("frozen actual input was not used", code, e)
	}
	if _, err := json.Marshal(prepared); err == nil || fmt.Sprintf("%+v", prepared) != "[REDACTED]" {
		t.Fatal("prepared input can leak through diagnostics")
	}
}
func TestInputTransportExpiryAndOwnedCopies(t *testing.T) {
	command, dependencies, _ := executionSnapshotFixture(t)
	prepared, e := PrepareInput(context.Background(), command, dependencies, []string{"BASE=before"}, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Release()
	encoded, e := prepared.Input.EncodeForLauncher()
	if e != nil {
		t.Fatal(e)
	}
	defer encoded.Destroy()
	copy, e := DecodeLauncherInput(encoded)
	if e != nil {
		t.Fatal(e)
	}
	defer copy.Destroy()
	raw, e := copy.Open()
	if e != nil {
		t.Fatal(e)
	}
	if len(raw.Patterns) != 1 || string(raw.Patterns[0]) != snapshotCredential {
		t.Fatal("actual output patterns missing")
	}
	raw.Environment[0] = "MUTATED=ignored"
	raw.Destroy()
	again, e := copy.Open()
	if e != nil {
		t.Fatal(e)
	}
	defer again.Destroy()
	if strings.HasPrefix(again.Environment[0], "MUTATED=") {
		t.Fatal("decoded copy mutated frozen input")
	}
	if _, err := json.Marshal(copy); err == nil || fmt.Sprintf("%#v", copy) != "[REDACTED]" {
		t.Fatal("payload can leak through diagnostics")
	}
	meta, e2 := json.Marshal(copy.Metadata())
	if e2 != nil || bytes.Contains(meta, []byte(snapshotCredential)) {
		t.Fatal("public metadata contains credential")
	}
	if e := copy.Validate(copy.Metadata().PrepareDeadline); e == nil || e.Code != "input_snapshot_expired" {
		t.Fatal("preparation timeout ignored")
	}
	copy.Destroy()
	if _, e := copy.Open(); e == nil {
		t.Fatal("destroyed payload reopened")
	}
}
func TestNoValueExecutionDoesNotUnlockAndParentPolicyIsIndependent(t *testing.T) {
	root := privateTempDir(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := Command{Project: project.Context{Profile: "app"}, Directory: root, Exec: []string{exe}, Inject: false}
	calls := 0
	dependencies := Dependencies{ValueSnapshot: func(context.Context, string, string, bool) (*values.Snapshot, *protocol.Error) {
		calls++
		return nil, protocol.NewError("vault_locked", "fixture locked", 3, nil)
	}}
	parent := []string{"PARENT_TOKEN=synthetic-parent", "DEVTOOLS_TASK_CONTEXT=synthetic-control"}
	for _, tc := range []struct {
		mode    process.ParentMode
		present bool
	}{{process.ParentInherit, true}, {process.ParentClean, false}} {
		prepared, e := PrepareInput(context.Background(), command, dependencies, parent, PlanOptions{Parent: process.ParentPolicy{Mode: tc.mode}})
		if e != nil {
			t.Fatal(e)
		}
		env := strings.Join(prepared.Environment([]string{"PARENT_TOKEN=later"}), "\n")
		prepared.Release()
		if strings.Contains(env, "PARENT_TOKEN") != tc.present || strings.Contains(env, "DEVTOOLS_TASK_CONTEXT") {
			t.Fatal("parent policy was coupled to stored-value injection")
		}
	}
	if calls != 0 {
		t.Fatal("no-value command accessed vault")
	}
}
func TestVersionProbeDoesNotReceiveKnownSecretOrControlContext(t *testing.T) {
	command, dependencies, _ := executionSnapshotFixture(t)
	command.Requirements = project.Requirements{Tools: map[string]project.Tool{"fixture": {Executable: command.Exec[0], Version: "1.2.3", VersionArgs: []string{"--synthetic-version"}}}}
	probes := 0
	ctx := process.WithRunner(context.Background(), func(_ context.Context, _ []string, _ string, env []string, _ io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
		probes++
		joined := strings.Join(env, "\n")
		if strings.Contains(joined, "TOKEN=") || strings.Contains(joined, "DEVTOOLS_TASK_CONTEXT=") {
			t.Fatal("probe inherited credential")
		}
		fmt.Fprint(out, "1.2.3")
		return 0, nil
	})
	prepared, e := PrepareInput(ctx, command, dependencies, []string{"TOKEN=synthetic-parent-token", "DEVTOOLS_TASK_CONTEXT=synthetic-control"}, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Release()
	if !ChecksPassed(prepared.Checks(ctx, []string{"TOKEN=late-parent-token"}, false)) || probes != 1 {
		t.Fatal("safe version probe failed")
	}
}
func TestSnapshotProviderCannotSilentlySupplyMetadataOrAnotherProfile(t *testing.T) {
	command, dependencies, _ := executionSnapshotFixture(t)
	for _, profile := range []string{"app", "other"} {
		// Obtain a valid legacy state through the normal isolated store.
		store := values.Store{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: profile}
		state, e := store.Read(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		dependencies.ValueSnapshot = func(context.Context, string, string, bool) (*values.Snapshot, *protocol.Error) {
			return values.CaptureLegacy(state, "", profile != "app")
		}
		if _, e := PrepareInput(context.Background(), command, dependencies, []string{}, PlanOptions{}); e == nil || e.Code != "invalid_input_snapshot" {
			t.Fatal("provider contract mismatch accepted")
		}
	}
	plan, e := ResolvePlan(command, []string{}, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer plan.Destroy()
	plan.created = time.Now().Add(-DefaultPreparationTimeout)
	if _, e := AuthenticateSnapshot(context.Background(), plan, dependencies); e == nil || e.Code != "input_snapshot_expired" {
		t.Fatal("expired plan accessed value provider")
	}
}
