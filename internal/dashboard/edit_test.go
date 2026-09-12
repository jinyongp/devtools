package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func editableFixture(t *testing.T) (tasks.Store, string) {
	t.Helper()
	s := tasks.Store{Directory: filepath.Join(t.TempDir(), "tasks"), Profile: "edit-fixture"}
	apply := func(action, target string, body tasks.Object, token string) tasks.Object {
		t.Helper()
		state, e := s.Read()
		if e != nil {
			t.Fatal(e)
		}
		out, e := s.Execute(context.Background(), tasks.Request{Action: action, Target: target, Body: body, Options: map[string]string{"request-id": tasks.ID(), "if-revision": fmt.Sprint(state.Revision), "context": token}})
		if e != nil {
			t.Fatal(action, e)
		}
		return out
	}
	w := apply("workstream.create", "", tasks.Object{"title": "Editable plan"}, "")["item"].(tasks.Object)["id"].(string)
	apply("spec.set", w, tasks.Object{"body": "Fixture specification", "requirements": []tasks.Object{{"key": "R", "text": "Editable"}}, "acceptance": []tasks.Object{{"key": "A", "text": "Works", "requirement_keys": []string{"R"}}}}, "")
	ids, vals := []string{}, []string{}
	for _, title := range []string{"Running task", "Completed task"} {
		id := apply("task.add", "", tasks.Object{"title": title, "workstream_id": w, "acceptance_keys": []string{"A"}}, "")["item"].(tasks.Object)["id"].(string)
		v := apply("validation.add", "", tasks.Object{"title": "Verify " + title, "task_id": id, "method": "fixture"}, "")["item"].(tasks.Object)["id"].(string)
		ids = append(ids, id)
		vals = append(vals, v)
	}
	apply("plan.set", w, tasks.Object{"body": "Fixture plan", "task_ids": ids, "validation_ids": vals}, "")
	apply("workstream.activate", w, tasks.Object{}, "")
	apply("run.claimed", ids[0], tasks.Object{}, "")
	claim := apply("run.claimed", ids[1], tasks.Object{}, "")
	token := claim["context"].(string)
	basis := apply("validation.basis", vals[1], tasks.Object{"code": []any{}}, token)
	apply("validation.record", vals[1], tasks.Object{"basis_id": basis["basis_id"], "result": "pass", "summary": "Fixture pass", "evidence": []any{}}, token)
	apply("task.completed", ids[1], tasks.Object{"summary": "Fixture complete"}, token)
	return s, w
}

func TestManagementEditPreviewAndExecutionBoundary(t *testing.T) {
	store, w := editableFixture(t)
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: store.Directory, sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	state, _ := store.Read()
	req := actionRequest{Domain: "task", Profile: store.Profile, Action: "workstream.edited", Target: w, Body: tasks.Object{"reason": "Edit while running", "operations": []tasks.Object{{"op": "plan.update", "value": tasks.Object{"body": "Changed plan"}}}}, Options: map[string]string{"if-revision": fmt.Sprint(state.Revision), "dry-run": "true"}}
	call := func() *httptest.ResponseRecorder {
		b, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", s.registry.Address+"/api/actions", strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", s.registry.Address)
		r.Header.Set("Authorization", "Bearer session")
		out := httptest.NewRecorder()
		s.ServeHTTP(out, r)
		return out
	}
	if out := call(); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	after, _ := store.Read()
	if after.Revision != state.Revision {
		t.Fatal("preview wrote")
	}
	delete(req.Options, "dry-run")
	req.Options["request-id"] = tasks.ID()
	if out := call(); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	state, _ = store.Read()
	req = actionRequest{Domain: "task", Profile: store.Profile, Action: "workstream.update", Target: w, Body: tasks.Object{"title": "Updated metadata"}, Options: map[string]string{"if-revision": fmt.Sprint(state.Revision), "request-id": tasks.ID()}}
	if out := call(); out.Code != 200 {
		t.Fatal("management metadata update", out.Code, out.Body)
	}
	state, _ = store.Read()
	if state.Items[w].Title != "Updated metadata" {
		t.Fatal("management metadata was not saved")
	}
	req.Options["if-revision"] = fmt.Sprint(state.Revision)
	req.Options["request-id"] = tasks.ID()
	if out := call(); out.Code != 409 || !strings.Contains(out.Body.String(), `"code":"no_change"`) || !strings.Contains(out.Body.String(), `"affected_count":0`) {
		t.Fatal("no_change management response", out.Code, out.Body)
	}
	for _, action := range []string{"run.synced", "run.claimed", "validation.basis", "validation.record", "validation.waive"} {
		req.Action = action
		if out := call(); out.Code != 400 {
			t.Fatal("execution boundary", action, out.Code)
		}
	}
}

// Opt-in visual fixture uses temporary data and never the user's task journal.
func TestEditBrowserFixture(t *testing.T) {
	output := os.Getenv("DEVTOOLS_EDIT_FIXTURE_OUTPUT")
	if output == "" {
		t.Skip("opt-in browser fixture")
	}
	store, _ := editableFixture(t)
	f, e := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	if e := ServeDevelopment(ctx, store.Directory, store.Profile, "assets", f); e != nil && ctx.Err() == nil {
		t.Fatal(e)
	}
}
