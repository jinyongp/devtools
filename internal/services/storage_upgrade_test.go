package services

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestStorageInspectionDeadlineBoundsRetainedBodyLockWait(t *testing.T) {
	root := privateTempDir(t)
	s := Store{Data: root}
	now := time.Now().UTC()
	if err := s.RestoreRecord(Record{ID: tasks.ID(), Profile: "app", Directory: root, Command: "web", CreatedAt: now, EndedAt: &now, State: "stopped"}); err != nil {
		t.Fatal(err)
	}
	prepare, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	prepare()
	release, err := maintenance.LockExclusive(context.Background(), filepath.Join(root, ".retained-bodies.lock"))
	if err != nil {
		t.Fatal(err)
	}
	release = sync.OnceFunc(release)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	events := make(chan progress.Event, 64)
	ctx = progress.WithObserver(ctx, func(event progress.Event) { events <- event })
	done := make(chan bool, 1)
	go func() {
		_, condition := (storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}).Inspect(ctx)
		done <- condition != nil
	}()
	select {
	case failed := <-done:
		if !failed {
			t.Fatal("storage inspection ignored its deadline")
		}
	case <-time.After(2 * time.Second):
		release()
		<-done
		t.Fatal("retained-body inspection waited beyond the caller's deadline")
	}
	close(events)
	found := false
	for event := range events {
		found = found || event.Message == "Waiting for retained data access; another operation holds the lock"
	}
	if !found {
		t.Fatal("the real blocked lock did not report its wait")
	}
}

func TestProcessFormatUpgradeIsExplicitResumableAndRejectsOldReader(t *testing.T) {
	root := privateTempDir(t)
	s := Store{Data: root}
	ctx := context.Background()
	now := time.Now().UTC()
	id := tasks.ID()
	r := Record{ID: id, Profile: "app-a", Directory: root, Command: "web", CreatedAt: now, EndedAt: &now, State: "stopped"}
	path := s.path(id, "record.json")
	raw, _ := json.Marshal(r)
	if err := maintenance.Write(path, raw); err != nil {
		t.Fatal(err)
	}
	if _, pe := s.StoredMetadata(); pe == nil || pe.Code != "migration_required" {
		t.Fatal("ordinary read converted old format", pe)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, raw) {
		t.Fatal("query changed legacy data")
	}
	engine := storageupgrade.Engine{Root: root, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}
	report, pe := engine.Fix(ctx)
	if pe != nil || !report.Ready || !report.Changed {
		t.Fatal(report, pe)
	}
	upgraded, _ := os.ReadFile(path)
	var old Record
	if err := tasks.ReadPrivate(path, &old); err == nil {
		t.Fatal("old strict reader accepted new process format")
	}
	report, pe = engine.Fix(ctx)
	if pe != nil || !report.Ready || report.Changed {
		t.Fatal(report, pe)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, upgraded) {
		t.Fatal("idempotent fix rewrote current format")
	}
	got, pe := s.Status(ctx, id)
	if pe != nil || got.ID != r.ID || got.State != r.State {
		t.Fatal(got, pe)
	}
	// A newer unknown version must not be overwritten by an ordinary writer.
	if err := maintenance.Write(path, []byte(`{"storage_version":99}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreRecord(r); err == nil {
		t.Fatal("writer overwrote newer storage format")
	}
	if got, _ := os.ReadFile(path); string(got) != `{"storage_version":99}` {
		t.Fatal("unknown format changed")
	}
}

func TestStorageUpgradeDoesNotConvertARecordWithLiveWriter(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	ctx := context.Background()
	r := Record{ID: tasks.ID(), Profile: "app-a", Directory: s.Data, CreatedAt: time.Now(), State: "running"}
	raw, _ := json.Marshal(r)
	if err := maintenance.Write(s.path(r.ID, "record.json"), raw); err != nil {
		t.Fatal(err)
	}
	lease, held, pe := s.claimLease(ctx, r.ID, true)
	if pe != nil || !held {
		t.Fatal(held, pe)
	}
	defer lease.unlockClose()
	engine := storageupgrade.Engine{Root: s.Data, Adapters: []storageupgrade.Adapter{StorageUpgradeAdapter()}}
	report, pe := engine.Fix(ctx)
	if pe != nil || report.Ready {
		t.Fatal(report, pe)
	}
	if got, _ := os.ReadFile(filepath.Join(s.root(), r.ID, "record.json")); !bytes.Equal(got, raw) {
		t.Fatal("active old writer was converted")
	}
}
