package values

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestManagedBatchImport(t *testing.T) {
	s := Store{Directory: filepath.Join(t.TempDir(), "profiles"), Profile: "test"}
	ctx := context.Background()
	view, e := s.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "existing"
	result, e := s.Apply(ctx, Change{Action: "variable.set", Key: "HOST", Value: &value, Revision: view.Revision, RequestID: tasks.ID()})
	if e != nil {
		t.Fatal(e)
	}
	result, e = s.Apply(ctx, Change{Action: "env.create", Env: "dev", Revision: result.Revision, RequestID: tasks.ID()})
	if e != nil {
		t.Fatal(e)
	}
	request := Change{Action: "import", Env: "dev", Revision: result.Revision, RequestID: tasks.ID(), Import: &BatchImport{Content: "HOST=new\nTOKEN=synthetic-private-value", Preview: true}}
	preview, e := s.Apply(ctx, request)
	if e != nil || !preview.Applicable || preview.Items[0].Action != "skip" {
		t.Fatalf("preview: %+v %v", preview, e)
	}
	view, _ = s.Inspect(ctx, "dev")
	if len(view.Items) != 1 || view.Revision != result.Revision {
		t.Fatal("preview mutated storage")
	}
	request.Import.Preview = false
	result, e = s.Apply(ctx, request)
	if e != nil || !result.Changed {
		t.Fatal(result, e)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "synthetic-private-value") {
		t.Fatal("value leaked")
	}
	view, _ = s.Inspect(ctx, "dev")
	for _, item := range view.Items {
		if item.Key == "HOST" && (*item.Value != "existing" || item.Source != "common") {
			t.Fatal("inherited value changed")
		}
	}
	if replay, e := s.Apply(ctx, request); e != nil || !replay.Replayed {
		t.Fatal("retry", e)
	}
	request.RequestID = tasks.ID()
	request.Revision = view.Revision
	request.Import.Overwrite = true
	result, e = s.Apply(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	view, _ = s.Inspect(ctx, "dev")
	for _, item := range view.Items {
		if item.Key == "HOST" && (*item.Value != "new" || item.Source != "env") {
			t.Fatal("override missing")
		}
	}
	request.RequestID = tasks.ID()
	request.Revision = result.Revision
	request.Import.Content = "NEW=value\nTOKEN=other"
	request.Import.Variables = []string{"TOKEN"}
	if _, e = s.Apply(ctx, request); e == nil || e.Code != "import_conflict" {
		t.Fatal("kind conflict accepted", e)
	}
	view, _ = s.Inspect(ctx, "dev")
	if len(view.Items) != 2 {
		t.Fatal("partial write")
	}
}
