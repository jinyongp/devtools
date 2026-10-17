package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func startingRecord(t *testing.T, s Store, id string, created time.Time) Record {
	t.Helper()
	record := Record{
		ID:        id,
		Profile:   "app",
		Directory: privateTempDir(t),
		Command:   "web",
		CreatedAt: created.UTC(),
		State:     "starting",
	}
	if err := writePrivate(s.path(id, "record.json"), record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestLegacyStartingGraceThenConvergesToInterrupted(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	freshID := tasks.ID()
	_ = startingRecord(t, s, freshID, time.Now().UTC())
	status, err := s.Status(context.Background(), freshID)
	if err != nil || status.State != "starting" || status.EndedAt != nil {
		t.Fatalf("fresh legacy start was not protected by grace: %#v %v", status, err)
	}
	result, err := s.start(context.Background(), Request{RequestID: freshID}, "")
	if err == nil || err.Code != "process_pending" || result.Item.State != "starting" {
		t.Fatalf("fresh legacy start did not remain pending: %#v %v", result, err)
	}

	staleID := tasks.ID()
	_ = startingRecord(t, s, staleID, time.Now().UTC().Add(-legacyStartupGrace-time.Second))
	result, err = s.start(context.Background(), Request{RequestID: staleID}, "")
	if err != nil || result.Item.State != "interrupted" || result.Item.Reason != "supervisor_lost" || result.Item.EndedAt == nil {
		t.Fatalf("stale legacy start did not converge: %#v %v", result, err)
	}
	replayed, err := s.start(context.Background(), Request{RequestID: staleID}, "")
	if err != nil || replayed.Item.State != "interrupted" || replayed.Item.EndedAt == nil {
		t.Fatalf("terminal legacy start did not replay: %#v %v", replayed, err)
	}
}

func TestLaunchMarkerUsesLeaseInsteadOfStartupGrace(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	record := Record{
		ID:        id,
		Profile:   "app",
		Directory: privateTempDir(t),
		Command:   "web",
		CreatedAt: time.Now().UTC(),
		State:     "starting",
	}
	lease, err := s.prepareLaunch(record)
	if err != nil {
		t.Fatal(err)
	}
	status, statusErr := s.Status(context.Background(), id)
	if statusErr != nil || status.State != "starting" || status.EndedAt != nil {
		t.Fatalf("held v1 lease did not protect startup: %#v %v", status, statusErr)
	}
	pending, pendingErr := s.start(context.Background(), Request{RequestID: id}, "")
	if pendingErr == nil || pendingErr.Code != "process_pending" || pending.Item.State != "starting" {
		t.Fatalf("held v1 lease did not keep retry pending: %#v %v", pending, pendingErr)
	}
	if err := lease.unlockClose(); err != nil {
		t.Fatal(err)
	}
	terminal, terminalErr := s.start(context.Background(), Request{RequestID: id}, "")
	if terminalErr != nil || terminal.Item.State != "interrupted" || terminal.Item.Reason != "supervisor_lost" || terminal.Item.EndedAt == nil {
		t.Fatalf("free v1 lease did not terminalize immediately: %#v %v", terminal, terminalErr)
	}
}

func TestLaunchMarkerMissingLeaseIsStorageError(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	record := Record{ID: id, Profile: "app", Directory: privateTempDir(t), Command: "web", CreatedAt: time.Now().UTC(), State: "starting"}
	lease, err := s.prepareLaunch(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.unlockClose(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.path(id, "lease.lock")); err != nil {
		t.Fatal(err)
	}
	if _, failure := s.Status(context.Background(), id); failure == nil || failure.Code != "io_error" {
		t.Fatalf("missing v1 lease was normalized instead of rejected: %v", failure)
	}
}

func TestCompletedReceiptReplaySkipsStagingCleanup(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	request := Request{Action: "start", RequestID: tasks.ID()}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(body))
	if err := writePrivate(filepath.Join(s.root(), "receipts", request.RequestID+".json"), receipt{
		Fingerprint: fingerprint,
		Done:        true,
		Result:      Result{Item: Record{ID: request.RequestID}, Changed: true},
	}); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(s.root(), launchStagingName(tasks.ID())+"abc")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stale, 0755); err != nil {
		t.Fatal(err)
	}

	result, failure := s.Apply(context.Background(), request)
	if failure != nil || !result.Replayed || !result.Changed || result.Item.ID != request.RequestID {
		t.Fatalf("completed replay was blocked by staging cleanup: %#v %v", result, failure)
	}
}

func TestExplicitStopBypassesLegacyStartupGrace(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	record := startingRecord(t, s, id, time.Now().UTC())
	result, err := s.stop(context.Background(), record)
	if err != nil || !result.Changed || result.Item.State != "interrupted" || result.Item.EndedAt == nil {
		t.Fatalf("explicit stop respected startup grace: %#v %v", result, err)
	}
}

func TestCleanupLaunchStagingUsesFixedPattern(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	if err := tasks.PrivateDir(s.root()); err != nil {
		t.Fatal(err)
	}
	id := tasks.ID()
	stale := filepath.Join(s.root(), launchStagingName(id)+"abc123")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(s.root(), ".launch-user-data")
	if err := os.Mkdir(unrelated, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupLaunchStaging(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale staging survived cleanup: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("non-staging directory was removed: %v", err)
	}
}
