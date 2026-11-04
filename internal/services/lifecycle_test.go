package services

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestControllerLossDoesNotReportSuccessfulCleanup(t *testing.T) {
	store := Store{Data: privateTempDir(t)}
	now := time.Now().UTC()
	record := Record{ID: tasks.ID(), Mode: "foreground", Profile: "app", Directory: privateTempDir(t), Command: "web", State: "running", CreatedAt: now, StartedAt: &now, Cleanup: CleanupStatus{Scope: ownedScope(), State: "not_requested"}}
	lease, condition := store.prepareLaunch(record)
	if condition != nil {
		t.Fatal(condition)
	}
	if err := lease.unlockClose(); err != nil {
		t.Fatal(err)
	}
	status, condition := store.Status(context.Background(), record.ID)
	if condition != nil || status.Control.State != "lost" || status.Control.Stop || status.Cleanup.State != "unknown" {
		t.Fatalf("lost controller was treated as controllable or clean: %#v %v", status, condition)
	}
	request := Request{Action: "stop", ID: record.ID, RequestID: tasks.ID()}
	for range 2 {
		result, condition := store.Apply(context.Background(), request)
		if condition == nil || condition.Code != "process_cleanup_unknown" || condition.Details["effects"] != "unknown" || result.Item.State != "interrupted" {
			t.Fatalf("stop claimed cleanup after controller loss: %#v %v", result, condition)
		}
	}
}

func TestStoredControlCapabilityCannotBecomeLiveAuthority(t *testing.T) {
	// A decoded stored response may contain available:true. Only a fresh,
	// authenticated observation can authorize that public capability again.
	var record Record
	if err := json.Unmarshal([]byte(`{"id":"11111111-1111-4111-8111-111111111111","mode":"foreground","state":"running","control":{"state":"available","stop":true}}`), &record); err != nil {
		t.Fatal(err)
	}
	public := record.publicContract()
	if public.Control.Stop || public.Control.State != "unavailable" || public.Cleanup.State != "unknown" {
		t.Fatal(public)
	}
	record.controlObserved = true
	public = record.publicContract()
	if !public.Control.Stop || public.Control.State != "available" || public.Cleanup.State != "unknown" {
		t.Fatal(public)
	}
}

func TestRecordedControlObservationSurvivesResultReplay(t *testing.T) {
	original := Record{ID: tasks.ID(), Mode: "foreground", State: "running", controlObserved: true, Cleanup: initialCleanup()}
	wire, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var recorded Record
	if err := json.Unmarshal(wire, &recorded); err != nil {
		t.Fatal(err)
	}
	replay, err := json.Marshal(recorded)
	if err != nil || !bytes.Equal(wire, replay) {
		t.Fatalf("historical observation changed during replay: %s != %s, %v", wire, replay, err)
	}
	if recorded.publicContract().Control.Stop {
		t.Fatal("historical observation became current control authority")
	}
}

func TestUnacknowledgedStopKeepsUnknownEffects(t *testing.T) {
	store := Store{Data: privateTempDir(t)}
	now := time.Now().UTC()
	record := Record{ID: tasks.ID(), Mode: "foreground", Profile: "app", Directory: privateTempDir(t), Command: "web", State: "running", CreatedAt: now, StartedAt: &now, Cleanup: initialCleanup()}
	lease, condition := store.prepareLaunch(record)
	if condition != nil {
		t.Fatal(condition)
	}
	defer lease.unlockClose()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	if err := writePrivate(store.path(record.ID, "control.json"), control{ID: record.ID, Address: server.URL, Token: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	result, condition := store.Apply(context.Background(), Request{Action: "stop", ID: record.ID, RequestID: tasks.ID()})
	if condition == nil || condition.Code != "process_cleanup_unknown" || condition.Details["effects"] != "unknown" || result.Item.EndedAt != nil || result.Item.Cleanup.State != "unknown" {
		t.Fatalf("a missing stop acknowledgement became a known no-effect outcome: %#v %v", result, condition)
	}
}

func TestModeSelectionPrecedesServiceAggregation(t *testing.T) {
	store := Store{Data: privateTempDir(t)}
	directory := privateTempDir(t)
	now := time.Now().UTC()
	for _, mode := range []string{"foreground", "managed"} {
		record := Record{ID: tasks.ID(), Mode: mode, Profile: "app", Directory: directory, Command: "web", State: "running", CreatedAt: now, StartedAt: &now, Cleanup: initialCleanup()}
		if err := writePrivate(store.path(record.ID, "record.json"), record); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(record) }))
		t.Cleanup(server.Close)
		if err := writePrivate(store.path(record.ID, "control.json"), control{ID: record.ID, Address: server.URL, Token: strings.Repeat("b", 64)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"foreground", "managed"} {
		rows, condition := store.Navigation(context.Background(), NavigationSelection{Profile: "app", Mode: mode}, []ServiceDeclaration{})
		if condition != nil || len(rows) != 1 || rows[0].State != "running" || rows[0].ActiveCount != 1 || rows[0].Mode != mode {
			t.Fatalf("mode selection was applied after aggregation: %#v %v", rows, condition)
		}
	}
}
