package hosts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func testManager(t *testing.T) (Manager, *int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if e := os.WriteFile(path, []byte("127.0.0.1 localhost\n# external content\n"), 0640); e != nil {
		t.Fatal(e)
	}
	prepareHostsTestFile(t, path)
	calls := new(int)
	m := Manager{Data: t.TempDir(), System: System{Path: path}}
	m.Run = func(ctx context.Context, kind string, input any, _ bool) *protocol.Error {
		if kind != "hosts" {
			t.Fatal(kind)
		}
		*calls++
		return m.System.Apply(ctx, input.(Request))
	}
	return m, calls
}
func TestHostsSharedOwnershipAndNoOp(t *testing.T) {
	m, calls := testManager(t)
	ctx := context.Background()
	mapping := []Mapping{{"app.test", "127.0.0.1"}}
	if e := m.Sync(ctx, "project/a/listener", mapping, ""); e != nil {
		t.Fatal(e)
	}
	if e := m.Sync(ctx, "global/b/listener", mapping, ""); e != nil {
		t.Fatal(e)
	}
	if *calls != 1 {
		t.Fatal("same mapping asked for privilege twice", *calls)
	}
	if e := m.Sync(ctx, "project/a/listener", nil, ""); e != nil {
		t.Fatal(e)
	}
	if !m.Prepared("global/b/listener", mapping[0]) || *calls != 1 {
		t.Fatal("shared mapping removed")
	}
	if e := m.Sync(ctx, "global/b/listener", nil, ""); e != nil {
		t.Fatal(e)
	}
	b, e := m.System.Read()
	if e != nil || string(b) != "127.0.0.1 localhost\n# external content\n" || *calls != 2 {
		t.Fatal(string(b), e, *calls)
	}
	assertHostsTestPermissions(t, m.System.Path)
}

func TestUnknownMarkedBlockIsPreservedBeforePrivilege(t *testing.T) {
	m, calls := testManager(t)
	before := "127.0.0.1 localhost\n" + begin + "\n127.0.0.1\tother-root.test\n" + end + "\n"
	if e := os.WriteFile(m.System.Path, []byte(before), 0640); e != nil {
		t.Fatal(e)
	}
	if _, e := m.PreviewChange("new-owner", []Mapping{{"new.test", "127.0.0.1"}}); e == nil || e.Code != "hosts_ownership_unknown" {
		t.Fatal(e)
	}
	if e := m.Sync(context.Background(), "new-owner", []Mapping{{"new.test", "127.0.0.1"}}, ""); e == nil || e.Code != "hosts_ownership_unknown" {
		t.Fatal(e)
	}
	after, e := m.System.Read()
	if e != nil || string(after) != before || *calls != 0 {
		t.Fatal("unknown ownership overwritten", string(after), e, *calls)
	}
}
func TestHostsExternalConflictStaleAndSymlink(t *testing.T) {
	m, calls := testManager(t)
	ctx := context.Background()
	mapping := []Mapping{{"localhost", "127.0.0.1"}}
	if e := m.Sync(ctx, "a", mapping, ""); e != nil || *calls != 0 {
		t.Fatal(e, *calls)
	}
	if e := m.Sync(ctx, "a", []Mapping{{"localhost", "::1"}}, ""); e == nil || e.Code != "hosts_conflict" {
		t.Fatal(e)
	}
	p, e := m.PreviewChange("b", []Mapping{{"app.test", "127.0.0.1"}})
	if e != nil {
		t.Fatal(e)
	}
	f, _ := os.OpenFile(m.System.Path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("# new external edit\n")
	f.Close()
	if e := m.Sync(ctx, "b", []Mapping{{"app.test", "127.0.0.1"}}, p["digest"].(string)); e == nil || e.Code != "stale_preview" {
		t.Fatal(e)
	}
	link := m.System.Path + "-link"
	if e := os.Symlink(m.System.Path, link); e != nil {
		t.Fatal(e)
	}
	if _, e := (System{Path: link}).Read(); e == nil {
		t.Fatal("followed symlink")
	}
	if _, e := Render([]byte("127.0.0.1 app.test\n192.0.2.1 app.test\n"), []Mapping{{"app.test", "127.0.0.1"}}); e == nil {
		t.Fatal("ignored second conflicting external mapping")
	}
}
func TestHostsLostResponseReconcilesAndIsolatedBlocks(t *testing.T) {
	m, calls := testManager(t)
	ctx := context.Background()
	run := m.Run
	first := true
	m.Run = func(ctx context.Context, k string, input any, tty bool) *protocol.Error {
		if e := run(ctx, k, input, tty); e != nil {
			return e
		}
		if first {
			first = false
			return failure("permission_required")
		}
		return nil
	}
	mapping := []Mapping{{"app.test", "127.0.0.1"}}
	if e := m.Sync(ctx, "a", mapping, ""); e == nil {
		t.Fatal("injected error missing")
	}
	if e := m.Sync(ctx, "a", mapping, ""); e != nil || *calls != 1 || !m.Prepared("a", mapping[0]) {
		t.Fatal(e, *calls)
	}
	m.Isolated = true
	if e := m.Sync(ctx, "b", mapping, ""); e == nil || e.Code != "isolated_operation_not_allowed" || *calls != 1 {
		t.Fatal(e, *calls)
	}
}
func TestHostsConcurrentOwners(t *testing.T) {
	m, _ := testManager(t)
	var wg sync.WaitGroup
	for _, key := range []string{"a", "b", "c"} {
		wg.Go(func() {
			if e := m.Sync(context.Background(), key, []Mapping{{"same.test", "127.0.0.1"}}, ""); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	b, e := m.System.Read()
	if e != nil || strings.Count(string(b), "same.test") != 1 {
		t.Fatal(string(b), e)
	}
}
