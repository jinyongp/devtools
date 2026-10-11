package netsetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func fakeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if e := os.WriteFile(path, []byte(content), 0755); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestSetupIdentityRetryAndIsolatedGuard(t *testing.T) {
	path := fakeExecutable(t, t.TempDir(), "devtools", "binary-one")
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	m := Manager{Data: t.TempDir(), Executable: path, GOOS: "linux", Run: func(_ context.Context, k string, input any, _ bool) *protocol.Error {
		calls++
		if k != "netsetup" || input.(Request).Identity.Path != canonicalPath {
			t.Fatal(k, input)
		}
		return nil
	}}
	id := tasks.ID()
	if _, e := m.Apply(context.Background(), id, true); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Apply(context.Background(), id, true); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	intent, e := m.ReadIntent()
	if e != nil || !intent.Enabled || intent.Preparing {
		t.Fatal(intent, e)
	}
	m.Isolated = true
	if _, e := m.Apply(context.Background(), tasks.ID(), true); e == nil || e.Code != "isolated_operation_not_allowed" || calls != 1 {
		t.Fatal(e, calls)
	}
}
func TestHelperRejectsPathReplacementBeforeCapabilityWrite(t *testing.T) {
	dir := t.TempDir()
	path := fakeExecutable(t, dir, "devtools", "one")
	f, identity, e := OpenIdentity(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	replacement := fakeExecutable(t, dir, "next", "two")
	if e := os.Rename(replacement, path); e != nil {
		t.Fatal(e)
	}
	if e := SystemApply(context.Background(), Request{Identity: identity, Enable: true}, path); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
}
func TestSetupInterruptedRequestDoesNotGrantReplacement(t *testing.T) {
	dir := t.TempDir()
	path := fakeExecutable(t, dir, "devtools", "one")
	calls := 0
	m := Manager{Data: t.TempDir(), Executable: path, GOOS: "linux", Run: func(context.Context, string, any, bool) *protocol.Error {
		calls++
		return failure("permission_required")
	}}
	id := tasks.ID()
	if _, e := m.Apply(context.Background(), id, true); e == nil {
		t.Fatal("expected failure")
	}
	if e := os.Rename(fakeExecutable(t, dir, "next", "two"), path); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Apply(context.Background(), id, true); e == nil || e.Code != "request_conflict" || calls != 1 {
		t.Fatal(e, calls)
	}
}

func TestReplacementReceiptsDistinguishFullInstallPath(t *testing.T) {
	m := Manager{Data: t.TempDir()}
	if m.replacementPath("/first/bin/devtools") == m.replacementPath("/second/bin/devtools") {
		t.Fatal("different installations shared a replacement receipt")
	}
}
func TestReplacementPrepareFailureAndCommitRollback(t *testing.T) {
	dir := t.TempDir()
	old := fakeExecutable(t, dir, "devtools", "old")
	next := fakeExecutable(t, dir, "candidate", "new")
	backup := filepath.Join(dir, "previous")
	if e := os.Link(old, backup); e != nil {
		t.Fatal(e)
	}
	f, identity, e := OpenIdentity(old)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	m := Manager{Data: t.TempDir(), Executable: next, GOOS: "linux", Run: func(context.Context, string, any, bool) *protocol.Error { return failure("permission_required") }}
	original := Intent{Identity: identity, Enabled: true}
	if e := m.writeIntent(original); e != nil {
		t.Fatal(e)
	}
	if e := m.PrepareReplacement(context.Background(), old); e == nil {
		t.Fatal("expected permission failure")
	}
	intent, e := m.ReadIntent()
	if e != nil || intent != original {
		t.Fatal(intent, e)
	}
	b, _ := os.ReadFile(old)
	if string(b) != "old" {
		t.Fatal(string(b))
	}
	m.Run = func(context.Context, string, any, bool) *protocol.Error { return nil }
	if e := m.PrepareReplacement(context.Background(), old); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(next, old); e != nil {
		t.Fatal(e)
	}
	if e := m.CommitReplacement(old, false); e != nil {
		t.Fatal(e)
	}
	intent, e = m.ReadIntent()
	if e != nil || intent.Identity.Digest == original.Identity.Digest || intent.Identity.Path != original.Identity.Path {
		t.Fatal(intent, e)
	}
	if e := os.Rename(backup, old); e != nil {
		t.Fatal(e)
	}
	if e := m.CommitReplacement(old, true); e != nil {
		t.Fatal(e)
	}
	intent, e = m.ReadIntent()
	if e != nil || intent != original {
		t.Fatal(intent, e)
	}
}
