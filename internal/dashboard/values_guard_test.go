package dashboard

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestDashboardValuesShareGuardedMutationContract(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	s := &Server{registry: Registry{Address: "http://127.0.0.1:1234"}, data: filepath.Join(root, "tasks"), sessions: map[string]time.Time{"session": time.Now().Add(time.Hour)}}
	// CurrentStore is also the CLI adapter's production store, not a substitute
	// test writer. Exercise both directions across the actual HTTP boundary.
	store := values.CurrentStore(filepath.Join(root, "profiles"), "app")
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	text := "synthetic-public-value"
	c := values.Change{Action: "variable.set", Key: "URL", Value: &text, Revision: view.Revision, RequestID: tasks.ID(), CreateOnly: true}
	post := func(c values.Change) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(actionRequest{Domain: "values", Profile: "app", Change: &c})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", s.registry.Address+"/api/actions", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", s.registry.Address)
		r.Header.Set("Authorization", "Bearer session")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if r := post(c); r.Code != 200 {
		t.Fatal("dashboard create-only rejected", r.Code)
	}
	if replay, e := store.Apply(ctx, c); e != nil || !replay.Replayed {
		t.Fatal("HTTP receipt did not replay through CLI store", e)
	}
	c.RequestID = tasks.ID()
	if _, e := store.Apply(ctx, c); e == nil || e.Code != "revision_conflict" {
		t.Fatal("HTTP edit did not invalidate CLI edit token", e)
	}
	view, e = store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	c.Revision, c.CreateOnly = view.Revision, false
	newText := "synthetic-newer-value"
	c.Value = &newText
	if _, e := store.Apply(ctx, c); e != nil {
		t.Fatal(e)
	}
	if r := post(c); r.Code != 200 || !strings.Contains(r.Body.String(), `"replayed":true`) {
		t.Fatal("CLI receipt did not replay through HTTP", r.Code)
	}
	c.RequestID = tasks.ID()
	if r := post(c); r.Code != 409 || !strings.Contains(r.Body.String(), "revision_conflict") {
		t.Fatal("CLI edit did not invalidate HTTP edit token", r.Code)
	}
	view, e = store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	c.Revision, c.CreateOnly = view.Revision, true
	if r := post(c); r.Code == 200 || !strings.Contains(r.Body.String(), "value_exists") {
		t.Fatal("HTTP writer bypassed exact-slot conflict", r.Code)
	}
}
