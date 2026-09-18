package profiles

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func writeDiffValues(t *testing.T, data, profile string, right bool) {
	t.Helper()
	store := values.Store{Directory: filepath.Join(data, "profiles"), Profile: profile}
	if _, err := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		changed := false
		for _, env := range []string{"local"} {
			itemChanged, itemErr := state.CreateEnv(env)
			if itemErr != nil {
				return false, itemErr
			}
			changed = changed || itemChanged
		}
		if right {
			itemChanged, itemErr := state.CreateEnv("staging")
			if itemErr != nil {
				return false, itemErr
			}
			changed = changed || itemChanged
		}
		variableValue := "LEFT_VARIABLE_CANARY"
		secretValue := "LEFT_SECRET_CANARY"
		if right {
			variableValue = "RIGHT_VARIABLE_CANARY"
			secretValue = "RIGHT_SECRET_CANARY"
		}
		itemChanged, itemErr := state.Set(values.Variable, "CONFIG", "", variableValue)
		if itemErr != nil {
			return false, itemErr
		}
		changed = changed || itemChanged
		if right {
			itemChanged, itemErr = state.Set(values.Variable, "CONFIG", "local", "RIGHT_OVERRIDE_CANARY")
			if itemErr != nil {
				return false, itemErr
			}
			changed = changed || itemChanged
		}
		itemChanged, itemErr = state.Set(values.Secret, "TOKEN", "", secretValue)
		if itemErr != nil {
			return false, itemErr
		}
		changed = changed || itemChanged
		if !right {
			itemChanged, itemErr = state.Set(values.Secret, "LEFT_ONLY", "", "LEFT_ONLY_SECRET_CANARY")
			if itemErr != nil {
				return false, itemErr
			}
			changed = changed || itemChanged
		}
		return changed, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func writeDiffTask(t *testing.T, data, profile, title string) {
	t.Helper()
	store := tasks.Store{Directory: filepath.Join(data, "tasks"), Profile: profile}
	if _, err := store.Execute(context.Background(), tasks.Request{Action: "task.add", Body: tasks.Object{"title": title}, Options: map[string]string{"request-id": tasks.ID()}}); err != nil {
		t.Fatal(err)
	}
}

func writeDiffInstance(t *testing.T, data, profile, directory, alias string) {
	t.Helper()
	store := ports.Store{Directory: filepath.Join(data, "ports")}
	if err := store.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		instance, registerErr := state.Register(profile, directory)
		if registerErr != nil {
			return false, registerErr
		}
		instance.Alias = &alias
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDiffIsStableMetadataOnlyAndIgnoresSecretValueEquality(t *testing.T) {
	data := t.TempDir()
	writeDiffValues(t, data, "left", false)
	writeDiffValues(t, data, "right", true)
	writeDiffTask(t, data, "left", "Left task")
	writeDiffTask(t, data, "right", "Right task")
	writeDiffInstance(t, data, "left", filepath.Join(data, "left-project"), "main")
	writeDiffInstance(t, data, "right", filepath.Join(data, "right-project"), "feature")

	diff, err := (Catalog{Data: data}).Diff("left", "right")
	if err != nil {
		t.Fatal(err)
	}
	if !diff.Different {
		t.Fatal("expected profiles to differ")
	}
	if len(diff.Envs) != 1 || diff.Envs[0].Name != "staging" || diff.Envs[0].Action != "added" {
		t.Fatalf("unexpected env diff: %#v", diff.Envs)
	}
	if len(diff.Variables) != 1 || diff.Variables[0].Key != "CONFIG" || diff.Variables[0].Action != "changed" {
		t.Fatalf("unexpected variable diff: %#v", diff.Variables)
	}
	if len(diff.Secrets) != 1 || diff.Secrets[0].Key != "LEFT_ONLY" || diff.Secrets[0].Action != "removed" {
		t.Fatalf("secret values must not create TOKEN changes: %#v", diff.Secrets)
	}
	if len(diff.Items) != 2 || diff.Items[0].Kind != "task" || diff.Items[1].Kind != "task" || diff.Items[0].ID > diff.Items[1].ID {
		t.Fatalf("unexpected task diff ordering: %#v", diff.Items)
	}
	if len(diff.Instances) != 2 || diff.Instances[0].Action != "removed" || diff.Instances[1].Action != "added" {
		t.Fatalf("unexpected instance diff: %#v", diff.Instances)
	}

	body, marshalErr := json.Marshal(diff)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, secret := range []string{"LEFT_VARIABLE_CANARY", "RIGHT_VARIABLE_CANARY", "RIGHT_OVERRIDE_CANARY", "LEFT_SECRET_CANARY", "RIGHT_SECRET_CANARY", "LEFT_ONLY_SECRET_CANARY"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("stored value leaked through diff: %s", secret)
		}
	}
	if strings.Contains(string(body), "processes") {
		t.Fatal("runtime process state leaked into canonical diff")
	}
}

func TestDiffMissingProfileFailsExplicitly(t *testing.T) {
	data := t.TempDir()
	writeDiffValues(t, data, "left", false)
	if _, err := (Catalog{Data: data}).Diff("left", "missing"); err == nil || err.Code != "profile_not_found" {
		t.Fatalf("expected profile_not_found, got %#v", err)
	}
}
