package hosts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
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
	m.intent = intent{RequestID: protocol.NewRequestID(), Operation: "test"}
	if e := m.Sync(ctx, "a", mapping, ""); e == nil {
		t.Fatal("injected error missing")
	}
	if e := m.ForRequest(protocol.NewRequestID(), "test", network.RouteRef{}, "").Sync(ctx, "a", mapping, ""); e == nil || e.Code != "recovery_required" || *calls != 1 {
		t.Fatal("different request adopted a pending write", e, *calls)
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

func TestHostsConcurrentSystemWriters(t *testing.T) {
	m, _ := testManager(t)
	before, e := m.System.Read()
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan *protocol.Error, 2)
	start := make(chan struct{})
	for _, host := range []string{"first.test", "second.test"} {
		go func() {
			<-start
			results <- m.System.Apply(context.Background(), Request{
				Expected: digest(before),
				Mappings: []Mapping{{host, "127.0.0.1"}},
			})
		}()
	}
	close(start)
	success, stale := 0, 0
	for range 2 {
		e := <-results
		switch {
		case e == nil:
			success++
		case e.Code == "stale_preview":
			stale++
		default:
			t.Fatal(e)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatal(success, stale)
	}
}

func TestHostsRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, fileState, action, write   string
		applied, drift, conflict, suffix bool
	}{
		{name: "before", fileState: "before", action: "apply_saved", write: "performed"},
		{name: "after", fileState: "after", action: "finalize", write: "not_performed", applied: true},
		{name: "rebase", fileState: "diverged", action: "rebase", write: "performed", drift: true},
		{name: "finalize drift", fileState: "diverged", action: "finalize", write: "not_performed", applied: true, drift: true},
		{name: "conflict", fileState: "diverged", action: "blocked", conflict: true},
		{name: "noncanonical block", fileState: "diverged", action: "blocked", applied: true, drift: true, suffix: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, calls := testManager(t)
			run := m.Run
			m.Run = func(ctx context.Context, kind string, request any, tty bool) *protocol.Error {
				if tc.applied {
					if e := run(ctx, kind, request, tty); e != nil {
						t.Fatal(e)
					}
				}
				return failure("permission_required")
			}
			ctx := context.Background()
			origin := protocol.NewRequestID()
			owned := m.ForRequest(origin, "up", network.RouteRef{}, "")
			desired := []Mapping{{"app.test", "127.0.0.1"}}
			if e := owned.Sync(ctx, "a", desired, ""); e == nil {
				t.Fatal("interruption was ignored")
			}
			edited := ""
			if tc.drift {
				edited = "# Keep this external edit\n"
			} else if tc.conflict {
				edited = "192.0.2.1 app.test\n"
			}
			if edited != "" {
				body, e := m.System.Read()
				if e != nil {
					t.Fatal(e)
				}
				current := append([]byte(edited), body...)
				if tc.suffix {
					current = append(body, edited...)
				}
				if err := os.WriteFile(m.System.Path, current, 0640); err != nil {
					t.Fatal(err)
				}
			}
			status := m.RecoveryStatus()
			if status.State != tc.fileState || status.RecoveryAction != tc.action || status.OriginRequestID != origin || status.StepID == "" {
				t.Fatal(status)
			}
			id := protocol.NewRequestID()
			m.Run = run
			if tc.action == "blocked" {
				if status.Digest != "" {
					t.Fatal("unsafe recovery offered", status)
				}
				return
			}
			if tc.action == "finalize" {
				m.Isolated = true
			}
			result, e := m.Recover(ctx, id, status.Digest)
			if e != nil || result.(map[string]any)["item"].(map[string]any)["os_write"] != tc.write {
				t.Fatal(result, e)
			}
			if *calls != 1 || m.RecoveryStatus().State != "none" || !m.Prepared("a", desired[0]) {
				t.Fatal(*calls, m.RecoveryStatus())
			}
			if e := owned.Sync(ctx, "a", desired, ""); e != nil || *calls != 1 {
				t.Fatal("original child did not replay", e, *calls)
			}
			result, e = m.Recover(ctx, id, status.Digest)
			if e != nil || result.(map[string]any)["replayed"] != true {
				t.Fatal("recovery replay", result, e)
			}
			if _, e := m.Recover(ctx, protocol.NewRequestID(), status.Digest); e == nil || e.Code != "stale_preview" {
				t.Fatal("new request accepted an expired digest", e)
			}
			body, issue := m.System.Read()
			if issue != nil || !strings.Contains(string(body), edited) {
				t.Fatal("external edit was lost", issue)
			}
		})
	}
}

func TestHostsRecoveryRetainsInterruptedRebaseApproval(t *testing.T) {
	m, calls := testManager(t)
	ctx := context.Background()
	run := m.Run
	m.Run = func(context.Context, string, any, bool) *protocol.Error { return failure("permission_required") }
	origin := m.ForRequest(protocol.NewRequestID(), "up", network.RouteRef{}, "")
	mapping := []Mapping{{"rebase.test", "127.0.0.1"}}
	if e := origin.Sync(ctx, "a", mapping, ""); e == nil {
		t.Fatal("expected pending request")
	}
	body, e := m.System.Read()
	if e != nil {
		t.Fatal(e)
	}
	if err := os.WriteFile(m.System.Path, append([]byte("# preserved\n"), body...), 0640); err != nil {
		t.Fatal(err)
	}
	first := m.RecoveryStatus()
	if first.RecoveryAction != "rebase" || first.Digest == "" {
		t.Fatal(first)
	}
	id := protocol.NewRequestID()
	m.Run = func(ctx context.Context, kind string, request any, terminal bool) *protocol.Error {
		if e := run(ctx, kind, request, terminal); e != nil {
			return e
		}
		return failure("permission_required")
	}
	if _, e := m.Recover(ctx, id, first.Digest); e == nil || e.Code != "recovery_required" {
		t.Fatal("lost helper response was reported as complete", e)
	}
	after := m.RecoveryStatus()
	if after.State != "after" || after.RecoveryAction != "finalize" || after.Digest != first.Digest || after.RecoveryRequestID != id {
		t.Fatal(after)
	}
	if e := origin.Sync(ctx, "a", mapping, ""); e == nil || e.Code != "recovery_required" {
		t.Fatal("origin adopted explicit rebase permission", e)
	}
	if _, e := m.Recover(ctx, protocol.NewRequestID(), after.Digest); e == nil || e.Code != "stale_preview" {
		t.Fatal("different recovery request adopted the approval", e)
	}
	result, e := m.Recover(ctx, id, first.Digest)
	if e != nil || result.(map[string]any)["item"].(map[string]any)["os_write"] != "unknown" || *calls != 1 {
		t.Fatal(result, e, *calls)
	}
}

func TestHostsRecoveryPublicOrigin(t *testing.T) {
	for _, visible := range []bool{true, false} {
		t.Run(map[bool]string{true: "public parent", false: "passthrough"}[visible], func(t *testing.T) {
			m, _ := testManager(t)
			parent := ""
			if visible {
				parent = protocol.NewRequestID()
			}
			child := protocol.NewRequestID()
			ref := network.RouteRef{Scope: "global", RouteID: protocol.NewRequestID()}
			listener := protocol.NewRequestID()
			m = m.WithOrigin(parent).ForRequest(child, "batch-forward", ref, "")
			m.Run = func(context.Context, string, any, bool) *protocol.Error { return failure("permission_required") }
			ctx := context.Background()
			if e := m.Sync(ctx, ref.Key()+"/"+listener, []Mapping{{"origin.test", "127.0.0.1"}}, ""); e == nil {
				t.Fatal("expected pending")
			}
			status := m.RecoveryStatus()
			if status.OriginRequestID != parent || status.ListenerID != listener || status.RouteRef == nil || *status.RouteRef != ref {
				t.Fatal(status)
			}
			if _, e := m.Recover(ctx, child, status.Digest); e == nil || e.Code != "invalid_argument" {
				t.Fatal("internal request ID accepted as explicit recovery", e)
			}
			if visible {
				if _, e := m.Recover(ctx, parent, status.Digest); e == nil || e.Code != "invalid_argument" {
					t.Fatal("public request ID accepted as explicit recovery", e)
				}
			}
		})
	}
}

func TestHostsObservation(t *testing.T) {
	a := Mapping{Host: "a.test", Address: "127.0.0.1"}
	b := Mapping{Host: "b.test", Address: "127.0.0.1"}
	old := []Owner{{"a", []Mapping{a}}, {"b", []Mapping{b}}}
	planned := old[1:]
	original := []byte("127.0.0.1 localhost\n")
	before, e := Render(original, mappings(old))
	if e != nil {
		t.Fatal(e)
	}
	after, e := Render(before, mappings(planned))
	if e != nil {
		t.Fatal(e)
	}
	st := state{Owners: old, Pending: &pending{Owners: planned, Request: Request{Expected: digest(before)}, After: digest(after)}}
	for _, body := range [][]byte{before, after, append([]byte("# external edit\n"), before...)} {
		o := observe(st, body, false)
		if issue := o.MappingIssue("b", b); issue != "" {
			t.Fatal(issue)
		}
		if issue := o.MappingIssue("a", a); issue != "hosts_recovery_required" {
			t.Fatal(issue)
		}
	}
	conflict := append([]byte("192.0.2.1 B.TEST\n"), before...)
	if issue := observe(st, conflict, false).MappingIssue("b", b); issue != "hosts_conflict" {
		t.Fatal(issue)
	}
	if issue := observe(st, conflict, true).MappingIssue("b", b); issue != "hosts_unavailable" {
		t.Fatal(issue)
	}
}
