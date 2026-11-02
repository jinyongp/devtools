package tasks

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func upgradeTaskFixture(t *testing.T, store Store) {
	t.Helper()
	report, condition := (storageupgrade.Engine{Root: filepath.Dir(store.Directory), Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}).Fix(context.Background())
	if condition != nil || !report.Ready {
		t.Fatal(report, condition)
	}
}

func TestExplicitStorageUpgradeHasNoDummyTaskMutation(t *testing.T) {
	store := fixture(t)
	id := ID()
	request := Request{Action: "task.add", Body: Object{"title": "preserved task"}, Options: map[string]string{"request-id": id}}
	first, condition := store.Execute(context.Background(), request)
	if condition != nil {
		t.Fatal(condition)
	}
	before := forceV2Storage(t, store)
	engine := storageupgrade.Engine{Root: filepath.Dir(store.Directory), Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}
	report, condition := engine.Fix(context.Background())
	if condition != nil || !report.Ready || !report.Changed {
		t.Fatal(report, condition)
	}
	journal, state, _, condition := store.loadResolved()
	if condition != nil {
		t.Fatal(condition)
	}
	if len(journal.Events) != len(before.Events) || state.Revision != len(before.Events) || !reflect.DeepEqual(journal.Contexts, before.Contexts) {
		t.Fatal("upgrade invented task history or contexts")
	}
	replayed, condition := store.Execute(context.Background(), request)
	if condition != nil || replayed["replayed"] != true || num(replayed, "revision") != num(first, "revision") || str(objectValue(replayed["item"]), "id") != itemID(first) {
		t.Fatal("upgrade lost exact request replay", replayed, condition)
	}
	report, condition = engine.Fix(context.Background())
	if condition != nil || !report.Ready || report.Changed {
		t.Fatal(report, condition)
	}
	release, err := maintenance.AcquirePassiveQuery(context.Background(), filepath.Dir(store.Directory))
	if err != nil {
		t.Fatal(err)
	}
	release()
}
