package services

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestExplicitProcessUpgradeResumesAfterCommittedStep(t *testing.T) {
	root := privateTempDir(t)
	s := Store{Data: root}
	now := time.Now().UTC()
	ids := []string{tasks.ID(), tasks.ID()}
	for _, id := range ids {
		r := Record{ID: id, Profile: "app-a", Directory: root, State: "stopped", CreatedAt: now, EndedAt: &now}
		raw, _ := json.Marshal(r)
		if err := maintenance.Write(s.path(id, "record.json"), raw); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	adapter := StorageUpgradeAdapter()
	apply := adapter.Apply
	committed := false
	adapter.Apply = func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		changed, err := apply(ctx, root, step)
		if changed && err == nil && !committed {
			committed = true
			cancel()
		}
		return changed, err
	}
	if _, condition := (storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{adapter}}).Fix(ctx); condition == nil || !committed {
		t.Fatal("fixture did not interrupt after publication", condition)
	}
	report, condition := (storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}).Fix(context.Background())
	if condition != nil || !report.Ready {
		t.Fatal(report, condition)
	}
	before := map[string][]byte{}
	for _, id := range ids {
		before[id], _ = os.ReadFile(s.path(id, "record.json"))
	}
	if _, pe := s.StoredMetadata(); pe != nil {
		t.Fatal(pe)
	}
	if _, pe := s.StoredRecords(); pe != nil {
		t.Fatal(pe)
	}
	for _, id := range ids {
		after, _ := os.ReadFile(s.path(id, "record.json"))
		if !bytes.Equal(before[id], after) {
			t.Fatal("ordinary reader rewrote current document")
		}
	}
}
