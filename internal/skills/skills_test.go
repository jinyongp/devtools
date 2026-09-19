package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, parent, name, frontmatter string, resources map[string]struct {
	body string
	mode os.FileMode
}) string {
	t.Helper()
	root := filepath.Join(parent, name)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}
	for path, resource := range resources {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(resource.body), resource.mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func skillDocument(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
}

func TestInspectParsesStandardFrontmatterAndTreeRevision(t *testing.T) {
	parent := t.TempDir()
	root := writeSkill(t, parent, "deploy-check", "---\nname: deploy-check\ndescription: |\n  Check deployment safety before release.\n  Use for deployment reviews.\nlicense: MIT\ncompatibility: Requires git\nmetadata:\n  owner: platform\nallowed-tools: Bash(git:*)\n---\n\n# Deploy\n", map[string]struct {
		body string
		mode os.FileMode
	}{
		"references/z.md":  {body: "reference-z", mode: 0644},
		"scripts/check.sh": {body: "#!/bin/sh\nexit 0\n", mode: 0755},
		"references/a.md":  {body: "reference-a", mode: 0644},
	})
	first, err := Inspect(root, Project)
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "deploy-check" || first.Scope != Project || !strings.Contains(first.Description, "deployment reviews") || first.License != "MIT" || first.Compatibility != "Requires git" || first.Metadata["owner"] != "platform" || first.AllowedTools != "Bash(git:*)" {
		t.Fatalf("skill = %#v", first)
	}
	if first.ResourceCount != 3 || len(first.Resources) != 3 || first.Resources[0].Path != "references/a.md" || first.Resources[2].Path != "scripts/check.sh" || !first.Resources[2].Executable {
		t.Fatalf("resources = %#v", first.Resources)
	}
	if first.Revision == "" || first.Content == "" {
		t.Fatalf("missing revision/content: %#v", first)
	}
	if err := os.WriteFile(filepath.Join(root, "references", "a.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := Inspect(root, Project)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision == first.Revision {
		t.Fatal("resource change did not change Skill revision")
	}
}

func TestInspectRejectsInvalidSkillContracts(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		body string
		code string
	}{
		{"missing description", "missing-description", "---\nname: missing-description\n---\nbody\n", "missing_field"},
		{"invalid name", "Bad_Name", "---\nname: Bad_Name\ndescription: bad\n---\n", "invalid_name"},
		{"name mismatch", "folder", "---\nname: other\ndescription: mismatch\n---\n", "name_mismatch"},
		{"non string description", "typed", "---\nname: typed\ndescription: true\n---\n", "invalid_field"},
		{"duplicate field", "duplicate", "---\nname: duplicate\nname: duplicate\ndescription: duplicate\n---\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			root := writeSkill(t, parent, tc.dir, tc.body, nil)
			_, err := Inspect(root, Project)
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v", err)
			}
			if tc.code != "" && validation.Code != tc.code {
				t.Fatalf("code = %q, want %q", validation.Code, tc.code)
			}
		})
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "invalid-utf8")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte{0xff, 0xfe}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(root, Project); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}

func TestInspectRejectsSymlinksAndOversizedResources(t *testing.T) {
	parent := t.TempDir()
	root := writeSkill(t, parent, "safe-skill", skillDocument("safe-skill", "Use for safe tests."), nil)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "reference.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(root, Project); err == nil {
		t.Fatal("symlink resource accepted")
	}
	if err := os.Remove(filepath.Join(root, "reference.md")); err != nil {
		t.Fatal(err)
	}
	oversized := make([]byte, MaxResourceBytes+1)
	if err := os.WriteFile(filepath.Join(root, "large.bin"), oversized, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(root, Project); err == nil {
		t.Fatal("oversized resource accepted")
	}
}

func TestDiscoverUsesProjectPrecedenceAndIsolatesInvalidSkills(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	userSkills := filepath.Join(home, ".agents", "skills")
	projectSkills := filepath.Join(project, ".agents", "skills")
	if err := os.MkdirAll(userSkills, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectSkills, 0755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, userSkills, "shared-skill", skillDocument("shared-skill", "User shared skill."), nil)
	writeSkill(t, userSkills, "user-only", skillDocument("user-only", "User only skill."), nil)
	projectRoot := writeSkill(t, projectSkills, "shared-skill", skillDocument("shared-skill", "Project shared skill."), nil)
	writeSkill(t, projectSkills, "broken", "---\nname: broken\n---\n", nil)

	catalog, err := Discover(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Items) != 2 || catalog.Items[0].Name != "shared-skill" || catalog.Items[0].Scope != Project || catalog.Items[0].Root != projectRoot || catalog.Items[1].Name != "user-only" {
		t.Fatalf("items = %#v", catalog.Items)
	}
	if len(catalog.Shadowed) != 1 || catalog.Shadowed[0].Name != "shared-skill" || catalog.Shadowed[0].ShadowedScope != User {
		t.Fatalf("shadowed = %#v", catalog.Shadowed)
	}
	if len(catalog.Diagnostics) != 1 || catalog.Diagnostics[0].Code != "missing_field" {
		t.Fatalf("diagnostics = %#v", catalog.Diagnostics)
	}
	if catalog.Items[0].Content != "" || catalog.Items[0].Resources != nil {
		t.Fatal("discovery loaded full Skill content")
	}
}

func TestRegisterCopiesProjectAndUserSkillsWithoutOverwrite(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	sourceParent := t.TempDir()
	source := writeSkill(t, sourceParent, "registered-skill", skillDocument("registered-skill", "Use when registration is tested."), map[string]struct {
		body string
		mode os.FileMode
	}{
		"scripts/run.sh":     {body: "#!/bin/sh\nexit 0\n", mode: 0755},
		"references/note.md": {body: "note", mode: 0644},
	})

	projectSkill, err := Register(t.Context(), source, project, home, Project)
	if err != nil {
		t.Fatal(err)
	}
	wantProject := filepath.Join(project, ".agents", "skills", "registered-skill")
	if projectSkill.Root != wantProject || projectSkill.Scope != Project {
		t.Fatalf("project Skill = %#v", projectSkill)
	}
	info, err := os.Stat(filepath.Join(wantProject, "scripts", "run.sh"))
	if err != nil || info.Mode().Perm()&0111 == 0 {
		t.Fatalf("executable mode = %v, %v", info, err)
	}
	if _, err := Register(t.Context(), source, project, home, Project); err == nil {
		t.Fatal("existing project Skill was overwritten")
	}

	userSourceParent := t.TempDir()
	userSource := writeSkill(t, userSourceParent, "user-skill", skillDocument("user-skill", "Use for user scope."), nil)
	userSkill, err := Register(t.Context(), userSource, project, home, User)
	if err != nil {
		t.Fatal(err)
	}
	if userSkill.Root != filepath.Join(home, ".agents", "skills", "user-skill") || userSkill.Scope != User {
		t.Fatalf("user Skill = %#v", userSkill)
	}
}

func TestRegisterRemovesReservedTargetWhenCanceled(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	sourceParent := t.TempDir()
	source := writeSkill(t, sourceParent, "cancel-skill", skillDocument("cancel-skill", "Use for cancellation tests."), map[string]struct {
		body string
		mode os.FileMode
	}{"references/a.md": {body: "a", mode: 0644}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Register(ctx, source, project, home, Project); err == nil {
		t.Fatal("canceled registration succeeded")
	}
	target := filepath.Join(project, ".agents", "skills", "cancel-skill")
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial target retained: %v", err)
	}
}

func TestDiscoverInvalidProjectRegistryDoesNotHideUserSkills(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	userSkills := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(userSkills, 0755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, userSkills, "user-safe", skillDocument("user-safe", "Use from user scope."), nil)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".agents")); err != nil {
		t.Fatal(err)
	}
	catalog, err := Discover(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Items) != 1 || catalog.Items[0].Name != "user-safe" {
		t.Fatalf("items = %#v", catalog.Items)
	}
	if len(catalog.Diagnostics) != 1 || catalog.Diagnostics[0].Scope != Project || catalog.Diagnostics[0].Code != "invalid_registry" {
		t.Fatalf("diagnostics = %#v", catalog.Diagnostics)
	}
}

func TestInspectRejectsOverlongCompatibility(t *testing.T) {
	parent := t.TempDir()
	body := "---\nname: compatibility-skill\ndescription: Use for compatibility checks.\ncompatibility: " + strings.Repeat("x", 501) + "\n---\n"
	root := writeSkill(t, parent, "compatibility-skill", body, nil)
	_, err := Inspect(root, Project)
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Code != "invalid_compatibility" {
		t.Fatalf("error = %#v", err)
	}
}
