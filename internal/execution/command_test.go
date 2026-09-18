package execution

import (
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
)

func TestResolveConfiguredUsesCommandDefaults(t *testing.T) {
	p := project.Context{
		Profile: "app",
		Root:    "/workspace/app",
		Requirements: project.Requirements{
			Tools: map[string]project.Tool{
				"go":  {Version: "1.27.1"},
				"git": {},
			},
			Vars: []string{"COMMON"},
		},
		Commands: map[string]project.Command{
			"web": {
				Exec:   []string{"go", "run", "."},
				Inject: true,
				Env:    "local",
				Requirements: project.Requirements{
					Tools: map[string]project.Tool{
						"go": {Version: "1.27.2"},
					},
					Vars: []string{"PORT"},
					Secs: []string{"TOKEN"},
				},
			},
		},
	}
	got, err := ResolveConfigured(p, "web", nil, []string{"--debug"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Project.Profile != "app" || got.Name != "web" || got.Directory != "/workspace/app" || got.Env != "local" || !got.Inject {
		t.Fatalf("unexpected effective command: %+v", got)
	}
	if !reflect.DeepEqual(got.Exec, []string{"go", "run", "."}) || !reflect.DeepEqual(got.ExtraArgs, []string{"--debug"}) || !reflect.DeepEqual(got.Arguments(), []string{"go", "run", ".", "--debug"}) {
		t.Fatalf("arguments = exec %#v extra %#v final %#v", got.Exec, got.ExtraArgs, got.Arguments())
	}
	if !reflect.DeepEqual(got.Requirements.Vars, []string{"COMMON", "PORT"}) || !reflect.DeepEqual(got.Requirements.Secs, []string{"TOKEN"}) {
		t.Fatalf("requirements = %+v", got.Requirements)
	}
	if got.Requirements.Tools["go"].Version != "1.27.2" {
		t.Fatalf("command tool did not override project tool: %+v", got.Requirements.Tools)
	}
	if _, ok := got.Requirements.Tools["git"]; !ok {
		t.Fatalf("project tool was lost: %+v", got.Requirements.Tools)
	}
}

func TestResolveConfiguredEnvironmentOverrideEnablesInjection(t *testing.T) {
	p := project.Context{Root: "/workspace/app", Commands: map[string]project.Command{
		"web": {Exec: []string{"server"}, Env: "default"},
	}}
	common := ""
	got, err := ResolveConfigured(p, "web", &common, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Env != "" || !got.Inject {
		t.Fatalf("explicit common environment = %+v", got)
	}
	override := "staging"
	got, err = ResolveConfigured(p, "web", &override, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Env != "staging" || !got.Inject {
		t.Fatalf("override = %+v", got)
	}
}

func TestResolveConfiguredClonesMutableDefinitionData(t *testing.T) {
	template := "http://example.invalid/"
	ready := &project.ReadyProbe{Exec: []string{"check", "ready"}}
	p := project.Context{
		Root: "/workspace/app",
		Commands: map[string]project.Command{
			"web": {
				Exec:  []string{"server"},
				Ready: ready,
				Serve: []string{"web"},
				Bind: map[string]project.Binding{
					"URL": {Template: &template},
				},
			},
		},
	}
	extra := []string{"child"}
	got, err := ResolveConfigured(p, "web", nil, extra)
	if err != nil {
		t.Fatal(err)
	}
	got.Exec[0] = "changed"
	got.ExtraArgs[0] = "changed"
	got.Serve[0] = "changed"
	got.Ready.Exec[0] = "changed"
	binding := got.Bind["URL"]
	*binding.Template = "changed"
	extra[0] = "caller-changed"

	definition := p.Commands["web"]
	if definition.Exec[0] != "server" || definition.Serve[0] != "web" || definition.Ready.Exec[0] != "check" || *definition.Bind["URL"].Template != template {
		t.Fatalf("effective command aliases project definition: %+v", definition)
	}
	if got.ExtraArgs[0] != "changed" {
		t.Fatalf("effective command aliases caller extra args: %#v", got.ExtraArgs)
	}
}

func TestResolveConfiguredRejectsUnknownCommand(t *testing.T) {
	_, err := ResolveConfigured(project.Context{Commands: map[string]project.Command{}}, "missing", nil, nil)
	if err == nil || err.Code != "command_not_found" || err.ExitCode != 3 || err.Details["command"] != "missing" {
		t.Fatalf("unexpected error: %+v", err)
	}
}
