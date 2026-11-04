package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/retainedbody"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestNavigationScaleReadsMetadataAndOnlySelectedLiveSupervisors(t *testing.T) {
	for _, count := range []int{313, 1000, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := Store{Data: privateTempDir(t)}
			dir := privateTempDir(t)
			now := time.Now().UTC()
			ended := now.Add(-time.Hour)
			// Historical bodies are deliberately absent: a navigation read must
			// succeed using metadata, independent of any historical payload IO.
			ref := retainedbody.Reference{Version: 1, Kind: "process_record", Digest: strings.Repeat("a", 64), Bytes: 1}
			for i := range count {
				id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
				r := Record{ID: id, Profile: "selected", Directory: dir, Command: "web", State: "stopped", CreatedAt: ended.Add(-time.Duration(i) * time.Second), EndedAt: &ended}
				// This fixture tests metadata-only navigation, not durable record
				// publication. Preserve private files without flushing 10,000
				// synthetic records through the production crash-safe writer.
				path := s.path(id, "record.json")
				if err := fixtureMkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				body, err := json.Marshal(diskRecord{Version: 2, Metadata: r, Body: ref})
				if err != nil {
					t.Fatal(err)
				}
				if err := fixtureWriteFile(path, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var selected, unrelated atomic.Int32
			add := func(profile string, hits *atomic.Int32) {
				id := tasks.ID()
				r := Record{ID: id, Profile: profile, Directory: dir, Command: "web", State: "running", CreatedAt: now, StartedAt: &now}
				if err := writePrivate(s.path(id, "record.json"), diskRecord{Version: 2, Metadata: r, Body: ref}); err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); _ = json.NewEncoder(w).Encode(r) }))
				t.Cleanup(server.Close)
				if err := writePrivate(s.path(id, "control.json"), control{ID: id, Address: server.URL, Token: strings.Repeat("f", 64)}); err != nil {
					t.Fatal(err)
				}
			}
			add("selected", &selected)
			add("unrelated", &unrelated)
			for _, live := range []int{1, 2} {
				if live == 2 {
					add("selected", &selected)
				}
				selected.Store(0)
				started := time.Now()
				rows, condition := s.Navigation(context.Background(), NavigationSelection{Profile: "selected"}, []ServiceDeclaration{})
				if condition != nil || len(rows) != 1 || rows[0].ActiveCount != live || selected.Load() != int32(live) || unrelated.Load() != 0 {
					t.Fatal(rows, condition, selected.Load(), unrelated.Load())
				}
				encoded, _ := json.Marshal(rows)
				if len(encoded) > 8000 {
					t.Fatal("current response grew with historical executions", len(encoded))
				}
				t.Logf("history=%d live=%d rpc=%d current_bytes=%d elapsed=%s", count, live, selected.Load(), len(encoded), time.Since(started))
			}
		})
	}
}

func TestProfileNamesDoesNotOpenHistoricalProcessBodies(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	now := time.Now().UTC()
	ref := retainedbody.Reference{Version: 1, Kind: "process_record", Digest: strings.Repeat("a", 64), Bytes: 1}
	for i, profile := range []string{"selected", "zeta", "selected"} {
		r := Record{ID: tasks.ID(), Profile: profile, Directory: privateTempDir(t), Command: "web", State: "stopped", CreatedAt: now.Add(-time.Hour), EndedAt: &now}
		disk := diskRecord{Version: 2, Metadata: r, Body: ref}
		if i == 1 {
			disk.RetiredAt = &now
		}
		if err := writePrivate(s.path(r.ID, "record.json"), disk); err != nil {
			t.Fatal(err)
		}
	}
	// The referenced bodies deliberately do not exist. Profile discovery depends
	// only on valid metadata, including records whose detail has been retired.
	names, condition := s.ProfileNames()
	if condition != nil || strings.Join(names, ",") != "selected,zeta" {
		t.Fatal(names, condition)
	}
}
