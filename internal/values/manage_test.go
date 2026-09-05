package values

import (
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedValuesRevisionReplayAndRedaction(t *testing.T) {
	ctx := context.Background()
	s := Store{Directory: filepath.Join(t.TempDir(), "profiles"), Profile: "app"}
	view, e := s.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	secret := "fixture-sensitive-value"
	change := Change{Action: "secret.set", Key: "TOKEN", Value: &secret, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000001"}
	result, e := s.Apply(ctx, change)
	if e != nil || !result.Changed {
		t.Fatal(result, e)
	}
	replay, e := s.Apply(ctx, change)
	if e != nil || !replay.Replayed || replay.Revision != result.Revision {
		t.Fatal(replay, e)
	}
	changed := change
	changed.Key = "OTHER"
	if _, e = s.Apply(ctx, changed); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
	changed.RequestID = "00000000-0000-4000-8000-000000000002"
	if _, e = s.Apply(ctx, changed); e == nil || e.Code != "revision_conflict" {
		t.Fatal(e)
	}
	view, e = s.Inspect(ctx, "")
	if e != nil || len(view.Items) != 1 || view.Items[0].Value != nil {
		t.Fatal(view, e)
	}
	b, _ := json.Marshal(view)
	if strings.Contains(string(b), secret) {
		t.Fatal("secret in view")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Directory), "backup-receipts", "*.json"))
	for _, file := range files {
		b, e := os.ReadFile(file)
		if e != nil || strings.Contains(string(b), secret) {
			t.Fatal("receipt contains value", e)
		}
	}
	_, e = s.Update(ctx, func(state *State) (bool, *protocol.Error) { return state.Set(Variable, "PUBLIC", "", "true") })
	if e != nil {
		t.Fatal(e)
	}
	changed.Revision = view.Revision
	if _, e = s.Apply(ctx, changed); e == nil || e.Code != "revision_conflict" {
		t.Fatal("CLI change not detected", e)
	}
	view, e = s.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, item := range view.Items {
		if item.Key == "PUBLIC" {
			found = item.Value != nil && *item.Value == "true"
		}
	}
	if !found {
		t.Fatal("variable not readable")
	}
}
