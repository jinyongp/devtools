package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestCompletionSelectsObservedCapabilitiesAndRetainedLogs(t *testing.T) {
	store := Store{Data: privateTempDir(t)}
	directory := privateTempDir(t)
	now, expired := time.Now().UTC(), time.Now().Add(-8*24*time.Hour)
	byToken := map[string]Record{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/status" {
			t.Error("completion invoked an operation", request.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		record, ok := byToken[strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(record)
	}))
	defer server.Close()
	records := map[string]Record{}
	for index, name := range []string{"foreground", "managed", "ready", "stopped", "lost", "logs", "expired"} {
		record := Record{ID: tasks.ID(), Profile: "app", Directory: directory, Command: name, Mode: "managed", State: "running", CreatedAt: now, StartedAt: &now, Cleanup: initialCleanup()}
		if name == "foreground" {
			record.Mode = "foreground"
		}
		record.ReadyConfigured = name == "ready"
		record.Capture = name == "logs" || name == "expired"
		if name == "stopped" || name == "expired" {
			record.EndedAt, record.State, record.Cleanup.State = &now, "stopped", "complete"
			if name == "expired" {
				record.EndedAt = &expired
			}
		}
		records[name] = record
		if issue := store.RestoreRecord(record); issue != nil {
			t.Fatal(issue)
		}
		if record.EndedAt == nil && name != "lost" {
			token := fmt.Sprintf("%064x", index+1)
			byToken[token] = record
			if err := writePrivate(store.path(record.ID, "control.json"), control{ID: record.ID, Address: server.URL, Token: token}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for action, wanted := range map[string][]string{
		"stop":    {"foreground", "managed", "ready", "logs"},
		"restart": {"foreground", "managed", "ready", "logs", "stopped", "expired"},
		"check":   {"ready"}, "wait": {"ready"}, "logs": {"logs"},
		"status": {"foreground", "managed", "ready", "stopped", "lost", "logs", "expired"},
	} {
		t.Run(action, func(t *testing.T) {
			items, issue := store.CompletionRecords(context.Background(), CompletionSelection{Action: action})
			if issue != nil {
				t.Fatal(issue)
			}
			got := []string{}
			for _, item := range items {
				got = append(got, item.Command)
			}
			sort.Strings(got)
			sort.Strings(wanted)
			if !reflect.DeepEqual(got, wanted) {
				t.Fatalf("%s: got %v want %v", action, got, wanted)
			}
		})
	}
	items, issue := store.CompletionRecords(context.Background(), CompletionSelection{Action: "stop", Prefix: records["foreground"].ID[:12], NavigationSelection: NavigationSelection{Profile: "app", Directory: directory}})
	if issue != nil || len(items) != 1 || items[0].ID != records["foreground"].ID {
		t.Fatal(items, issue)
	}
}

func TestActiveCompletionSkipsIndexedHistoryAndObservesNewControllers(t *testing.T) {
	ctx := context.Background()
	store := Store{Data: privateTempDir(t)}
	now := time.Now().UTC()
	ended := Record{ID: tasks.ID(), Profile: "app", Directory: store.Data, Command: "old", State: "exited", CreatedAt: now, EndedAt: &now}
	if err := store.RestoreRecord(ended); err != nil {
		t.Fatal(err)
	}
	if e := store.PrepareCompletionIndex(ctx); e != nil {
		t.Fatal(e)
	}
	// A non-readable old artifact distinguishes skipping the metadata read from
	// loading it and filtering it out afterward.
	if err := os.WriteFile(store.path(ended.ID, "record.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	live := Record{ID: tasks.ID(), Profile: "app", Directory: store.Data, Command: "old-build", State: "running", CreatedAt: now}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("completion tried to invoke %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(live)
	}))
	defer server.Close()
	if err := store.RestoreRecord(live); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(store.path(live.ID, "control.json"), control{ID: live.ID, Address: server.URL, Token: strings.Repeat("f", 64)}); err != nil {
		t.Fatal(err)
	}
	items, e := store.CompletionRecords(ctx, CompletionSelection{Action: "stop"})
	if e != nil || len(items) != 1 || items[0].ID != live.ID {
		t.Fatal(items, e)
	}
}
