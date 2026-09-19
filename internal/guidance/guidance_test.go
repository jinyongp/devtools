package guidance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAgents(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, Filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveOrdersRootAndNestedGuidanceByActualTarget(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "packages", "api")
	sibling := filepath.Join(root, "packages", "web")
	if err := os.MkdirAll(filepath.Join(nested, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0755); err != nil {
		t.Fatal(err)
	}
	writeAgents(t, root, "root guidance\n")
	writeAgents(t, nested, "api guidance\n")
	writeAgents(t, sibling, "web guidance\n")
	target := filepath.Join(nested, "src", "handler.go")
	if err := os.WriteFile(target, []byte("package api\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Resolve(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || result.Target != "packages/api/src/handler.go" || result.TargetDir != "packages/api/src" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Sources) != 2 ||
		result.Sources[0].Path != "AGENTS.md" || result.Sources[0].Scope != "." ||
		result.Sources[1].Path != "packages/api/AGENTS.md" || result.Sources[1].Scope != "packages/api" {
		t.Fatalf("sources = %#v", result.Sources)
	}
	if result.Sources[0].Content != "root guidance\n" || result.Sources[1].Content != "api guidance\n" {
		t.Fatalf("contents = %#v", result.Sources)
	}

	siblingResult, err := Resolve(root, sibling)
	if err != nil {
		t.Fatal(err)
	}
	if len(siblingResult.Sources) != 2 || siblingResult.Sources[1].Content != "web guidance\n" {
		t.Fatalf("sibling sources = %#v", siblingResult.Sources)
	}
}

func TestResolveSupportsExistingDirectoryAndNonexistentNewFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkg", "nested")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeAgents(t, root, "root\n")
	writeAgents(t, filepath.Join(root, "pkg"), "pkg\n")

	existing, err := Resolve(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	if existing.Target != "pkg/nested" || existing.TargetDir != "pkg/nested" || len(existing.Sources) != 2 {
		t.Fatalf("directory result = %#v", existing)
	}

	newFile, err := Resolve(root, filepath.Join(dir, "future", "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if newFile.Target != "pkg/nested/future/new.go" || newFile.TargetDir != "pkg/nested/future" || len(newFile.Sources) != 2 {
		t.Fatalf("new file result = %#v", newFile)
	}
}

func TestResolveRefreshesRevisionWithoutCache(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	rootAgents := writeAgents(t, root, "v1\n")

	first, err := Resolve(root, filepath.Join(nested, "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootAgents, []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(root, filepath.Join(nested, "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == second.Revision || first.Sources[0].Revision == second.Sources[0].Revision {
		t.Fatal("changed AGENTS.md did not refresh revision")
	}

	writeAgents(t, nested, "nested\n")
	third, err := Resolve(root, filepath.Join(nested, "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Sources) != 2 || third.Revision == second.Revision {
		t.Fatalf("new ancestor was not discovered: %#v", third)
	}
	if err := os.Remove(filepath.Join(nested, Filename)); err != nil {
		t.Fatal(err)
	}
	fourth, err := Resolve(root, filepath.Join(nested, "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fourth.Sources) != 1 || fourth.Revision == third.Revision {
		t.Fatalf("removed ancestor was not refreshed: %#v", fourth)
	}
}

func TestResolveRejectsProjectBoundaryAndSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, err := Resolve(root, filepath.Join(outside, "file.go")); err == nil {
		t.Fatal("absolute outside target accepted")
	}
	if _, err := Resolve(root, filepath.Join("..", filepath.Base(outside), "file.go")); err == nil {
		t.Fatal("relative traversal accepted")
	}

	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, filepath.Join(link, "future.go")); err == nil {
		t.Fatal("symlink target escape accepted")
	}

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(link, 0755); err != nil {
		t.Fatal(err)
	}
	outsideAgents := writeAgents(t, outside, "outside\n")
	if err := os.Symlink(outsideAgents, filepath.Join(link, Filename)); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, filepath.Join(link, "future.go")); err == nil || !strings.Contains(err.Error(), "symlink escapes") {
		t.Fatalf("AGENTS.md symlink escape error = %v", err)
	}
}

func TestResolveAcceptsConfinedAgentsSymlinkAndEmptyFile(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	nested := filepath.Join(root, "pkg")
	if err := os.MkdirAll(shared, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	sharedAgents := writeAgents(t, shared, "shared guidance\n")
	if err := os.Symlink(sharedAgents, filepath.Join(nested, Filename)); err != nil {
		t.Fatal(err)
	}
	writeAgents(t, root, "")

	result, err := Resolve(root, filepath.Join(nested, "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Sources) != 2 || result.Sources[0].Content != "" || result.Sources[1].Content != "shared guidance\n" {
		t.Fatalf("result = %#v", result)
	}
}

func TestResolveReportsInvalidUTF8AndOversizedSourcesAsIncomplete(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	writeAgents(t, root, "root\n")
	if err := os.WriteFile(filepath.Join(nested, Filename), []byte{0xff, 0xfe}, 0644); err != nil {
		t.Fatal(err)
	}
	result, err := Resolve(root, filepath.Join(nested, "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || len(result.Sources) != 1 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "invalid_utf8" {
		t.Fatalf("invalid UTF-8 result = %#v", result)
	}

	large := strings.Repeat("x", MaxSourceBytes+1)
	if err := os.WriteFile(filepath.Join(nested, Filename), []byte(large), 0644); err != nil {
		t.Fatal(err)
	}
	result, err = Resolve(root, filepath.Join(nested, "new.go"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "oversized" {
		t.Fatalf("oversized result = %#v", result)
	}
}

func TestResolveWorktreesHaveIndependentRevisions(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeAgents(t, first, "first\n")
	writeAgents(t, second, "second\n")

	left, err := Resolve(first, "new.go")
	if err != nil {
		t.Fatal(err)
	}
	right, err := Resolve(second, "new.go")
	if err != nil {
		t.Fatal(err)
	}
	if left.Revision == right.Revision || left.Sources[0].Revision == right.Sources[0].Revision {
		t.Fatal("independent worktrees shared guidance revision")
	}
}

func TestResolveRejectsSpecialAgentsFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, Filename), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(root, filepath.Join(dir, "new.go"))
	var boundary *BoundaryError
	if !errors.As(err, &boundary) {
		t.Fatalf("special AGENTS.md error = %v", err)
	}
}
