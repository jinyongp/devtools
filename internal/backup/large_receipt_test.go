package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestLargeImportReceiptReplaysCompleteResult(t *testing.T) {
	root := t.TempDir()
	source := Engine{Data: filepath.Join(root, "source", "data"), Config: filepath.Join(root, "source", "config"), Cache: filepath.Join(root, "source", "cache")}
	target := Engine{Data: filepath.Join(root, "target", "data"), Config: filepath.Join(root, "target", "config"), Cache: filepath.Join(root, "target", "cache")}
	ctx := context.Background()
	identity, recipient := filepath.Join(root, "identity"), filepath.Join(root, "recipient")
	if _, e := Keygen(identity, recipient); e != nil {
		t.Fatal(e)
	}
	store := values.Store{Directory: filepath.Join(source.Data, "profiles"), Profile: "portable"}
	if _, e := store.Update(ctx, func(s *values.State) (bool, *protocol.Error) {
		for i := 0; i < 6500; i++ {
			if _, e := s.Set(values.Secret, fmt.Sprintf("K%063d", i), "", "dummy"); e != nil {
				return false, e
			}
		}
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	archive := filepath.Join(root, "portable.age")
	if _, e := source.Create(ctx, "portable", archive, recipient); e != nil {
		t.Fatal(e)
	}
	preview, e := target.Import(ctx, ImportRequest{Path: archive, IdentityPath: identity})
	if e != nil {
		t.Fatal(e)
	}
	request := ImportRequest{Path: archive, IdentityPath: identity, Expected: preview.Plan.Digest, RequestID: tasks.ID()}
	applied, e := target.Import(ctx, request)
	if e != nil || !applied.Plan.Applied || applied.Plan.Replayed {
		t.Fatal(applied.Plan, e)
	}
	receipt, e2 := os.Stat(filepath.Join(target.Data, "backup-receipts", request.RequestID+".json"))
	if e2 != nil {
		t.Fatal(e2)
	}
	if receipt.Size() <= 1<<20 {
		t.Fatal("fixture must exceed the old receipt read limit")
	}
	replay, e := target.Import(ctx, request)
	if e != nil || !replay.Plan.Applied || !replay.Plan.Replayed || replay.Plan.Digest != applied.Plan.Digest || !reflect.DeepEqual(replay.Diff, applied.Diff) || replay.Target != applied.Target || replay.Source != applied.Source {
		t.Fatal("large import replay changed result", e)
	}
	state, e := (values.Store{Directory: filepath.Join(target.Data, "profiles"), Profile: "portable"}).Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	env, e := state.Environment("")
	if e != nil || len(env) != 6500 {
		t.Fatal("imported keys missing", len(env), e)
	}
}
