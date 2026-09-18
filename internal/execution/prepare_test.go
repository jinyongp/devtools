package execution

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func TestPrepareInjectsSelectedEnvironment(t *testing.T) {
	root := t.TempDir()
	valuesDir := filepath.Join(root, "profiles")
	store := values.Store{Directory: valuesDir, Profile: "app"}
	if _, err := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		changed, e := state.CreateEnv("local")
		if e != nil {
			return false, e
		}
		set, e := state.Set(values.Variable, "MESSAGE", "local", "hello")
		return changed || set, e
	}); err != nil {
		t.Fatal(err)
	}
	p := project.Context{
		Profile: "app",
		Root:    root,
		Commands: map[string]project.Command{
			"web": {Exec: []string{"server"}, Env: "local", Inject: true},
		},
	}
	command, err := ResolveConfigured(p, "web", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, prepErr := Prepare(context.Background(), command, Dependencies{ValuesDirectory: valuesDir}, false)
	if prepErr != nil {
		t.Fatal(prepErr)
	}
	defer prepared.Release()
	if prepared.Injected["MESSAGE"] != "hello" {
		t.Fatalf("injected = %#v", prepared.Injected)
	}
	env := prepared.Environment([]string{"BASE=parent", "MESSAGE=parent"})
	if !reflect.DeepEqual(env, []string{"BASE=parent", "MESSAGE=hello"}) {
		t.Fatalf("environment = %#v", env)
	}
}

func TestPrepareExpandsConfiguredExecButNotExtraArgs(t *testing.T) {
	root := t.TempDir()
	port := 26123
	ref := "$" + "{" + "bind.PORT}"
	p := project.Context{
		Profile: "app",
		Root:    root,
		Ports: map[string]project.Port{
			"web": {Port: &port},
		},
		Commands: map[string]project.Command{
			"web": {
				Exec:  []string{"server", ref},
				Serve: []string{"web"},
				Bind: map[string]project.Binding{
					"PORT": {Port: "web"},
				},
			},
		},
	}
	command, err := ResolveConfigured(p, "web", nil, []string{ref})
	if err != nil {
		t.Fatal(err)
	}
	prepared, prepErr := Prepare(context.Background(), command, Dependencies{
		ValuesDirectory: filepath.Join(root, "profiles"),
		Ports:           ports.Store{Directory: filepath.Join(root, "ports")},
		PortDefaults:    []int{26000, 26999},
	}, true)
	if prepErr != nil {
		t.Fatal(prepErr)
	}
	defer prepared.Release()
	if !reflect.DeepEqual(prepared.Args, []string{"server", "26123", ref}) {
		t.Fatalf("args = %#v", prepared.Args)
	}
	if prepared.Injected["PORT"] != "26123" {
		t.Fatalf("injected = %#v", prepared.Injected)
	}
}

func TestChecksSkipExecutableWithoutRequirementsUnlessForced(t *testing.T) {
	command := Command{
		Project:   project.Context{Profile: "app"},
		Directory: t.TempDir(),
		Exec:      []string{"definitely-missing-devtools-test-executable"},
	}
	prepared := Prepared{Command: command, Args: command.Arguments()}
	if checks := prepared.Checks(context.Background(), nil, false); len(checks) != 0 {
		t.Fatalf("foreground checks = %#v", checks)
	}
	checks := prepared.Checks(context.Background(), nil, true)
	if len(checks) != 1 || checks[0].ID != "command_executable" || checks[0].Status != "fail" {
		t.Fatalf("forced checks = %#v", checks)
	}
	if ChecksPassed(checks) {
		t.Fatal("failed checks reported as passed")
	}
}

func TestPreflightValidatesSelectedEnvBeforeRequirementChecks(t *testing.T) {
	command := Command{
		Project:      project.Context{Profile: "app"},
		Name:         "web",
		Exec:         []string{"/bin/true"},
		Directory:    t.TempDir(),
		Env:          "missing",
		Requirements: project.Requirements{Vars: []string{"REQUIRED"}},
	}
	err := Preflight(context.Background(), command, Dependencies{
		ValuesDirectory: filepath.Join(t.TempDir(), "profiles"),
	}, nil)
	if err == nil || err.Code != "env_not_found" {
		t.Fatalf("unexpected preflight error: %v", err)
	}
}
