package execution

import (
	"context"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/vault"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleSelectionPrecedenceAndEnvDoesNotExpandNone(t *testing.T) {
	p := project.Context{Profile: "app", Root: privateTempDir(t), Bundle: "frontend", Commands: map[string]project.Command{
		"default": {Exec: []string{"fixture"}, Inject: true}, "private": {Exec: []string{"fixture"}, Inject: true, Bundle: "backend"}, "disabled": {Exec: []string{"fixture"}, Bundle: "none"},
	}}
	env := "local"
	for _, tc := range []struct{ name, override, want string }{{"default", "", "frontend"}, {"private", "", "backend"}, {"private", "none", "none"}, {"disabled", "", "none"}, {"default", "all", "all"}} {
		c, e := ResolveConfigured(p, tc.name, &env, nil)
		if e != nil {
			t.Fatal(e)
		}
		plan, e := ResolvePlan(c, nil, PlanOptions{Selection: tc.override})
		if e != nil {
			t.Fatal(e)
		}
		if plan.Summary().Selection != tc.want || !plan.Summary().Inject || plan.Summary().Env != "local" {
			t.Fatal("selection/env precedence", plan.Summary())
		}
		plan.Destroy()
	}
	p.Bundle = ""
	c, e := ResolveConfigured(p, "default", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	plan, e := ResolvePlan(c, nil, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer plan.Destroy()
	if plan.Summary().Selection != "all" {
		t.Fatal("old config changed")
	}
}

func TestBundleExecutionUsesSelectedPublicSnapshotAndHandoff(t *testing.T) {
	root := privateTempDir(t)
	directory := filepath.Join(root, "profiles")
	// Publish a complete current-format public fixture with its downgrade marker. No
	// native vault or passphrase is needed by an all-public project.
	p, err := vault.NewProfile("app")
	if err != nil {
		t.Fatal(err)
	}
	p.Set("PUBLIC", vault.Public, "", "selected", nil)
	p.Set("OTHER", vault.Public, "", "outside", nil)
	p.SetBundle("frontend", []string{"PUBLIC"}, nil)
	r, err := platformfs.OpenRoot(directory, true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.MkdirAll(".identity"); err != nil {
		t.Fatal(err)
	}
	identity, err := profilekey.IdentityBytes("app", "values")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.WriteFile(context.Background(), ".identity/"+profilekey.CanonicalFilename("app"), identity, false); err != nil {
		t.Fatal(err)
	}
	encoded, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err = r.WriteFile(context.Background(), profilekey.CanonicalFilename("app"), encoded, false); err != nil {
		t.Fatal(err)
	}
	marker, err := profilekey.MarkerBytes("app")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.WriteFile(context.Background(), profilekey.LegacyFilename("app"), marker, false); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := Command{Project: project.Context{Profile: "app", Root: root}, Directory: root, Exec: []string{exe}, Inject: true, Selection: "frontend"}
	deps, e := DependenciesFor(command, root, "")
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := PrepareInput(context.Background(), command, deps, nil, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer prepared.Release()
	if len(prepared.Injected) != 1 || prepared.Injected["PUBLIC"] != "selected" || prepared.Input.Metadata().Selection != "frontend" {
		t.Fatal("selected payload expanded")
	}
	outside := "${var.OTHER}"
	command.Bind = map[string]project.Binding{"URL": {Template: &outside}}
	if _, e := PrepareInput(context.Background(), command, deps, nil, PlanOptions{}); e == nil {
		t.Fatal("binding read outside selected bundle")
	}
	command.Bind = nil
	command.Selection = "none"
	empty, e := PrepareInput(context.Background(), command, deps, nil, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer empty.Release()
	if len(empty.Injected) != 0 || empty.Input.Metadata().Selection != "none" {
		t.Fatal("none injected values")
	}
	command.Selection = "missing"
	if _, e := PrepareInput(context.Background(), command, deps, nil, PlanOptions{}); e == nil || e.Code != "bundle_not_found" {
		t.Fatal("missing selection fell back", e)
	}
}
