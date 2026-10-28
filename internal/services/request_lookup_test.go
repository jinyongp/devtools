package services

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestInspectRequestIsPassiveHistoricalResult(t *testing.T) {
	s := Store{Data: privateTempDir(t)}
	id := tasks.ID()
	if _, e := s.InspectRequest(id); e == nil || e.Code != "request_not_found" {
		t.Fatal(e)
	}
	if _, e := os.Stat(s.root()); !os.IsNotExist(e) {
		t.Fatal("query initialized storage", e)
	}
	now := time.Now().UTC()
	for _, done := range []bool{false, true} {
		value := receipt{Fingerprint: strings.Repeat("a", 64), Done: done}
		if done {
			value.Result = Result{Changed: true, Item: Record{ID: tasks.ID(), State: "failed", EndedAt: &now, Reason: "execution_failed"}}
		}
		path := filepath.Join(s.root(), "receipts", id+".json")
		if e := writePrivate(path, value); e != nil {
			t.Fatal(e)
		}
		before, _ := os.ReadFile(path)
		got, e := s.InspectRequest(id)
		if e != nil || got.RequestID != id || (got.State == "completed") != done {
			t.Fatal(got, e)
		}
		if done && (got.Result == nil || got.Result.Item.State != "failed" || got.Result.Item.ID == id) {
			t.Fatal("lost reused execution/failure", got)
		}
		if !done && got.Result != nil {
			t.Fatal("empty pending receipt became a result")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("query rewrote receipt")
		}
	}
	if e := writePrivate(filepath.Join(s.root(), "receipts", id+".json"), map[string]any{}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.InspectRequest(id); e == nil || e.Code == "request_not_found" {
		t.Fatal("corruption hidden", e)
	}
}

func TestLegacyRequestProjectionAndReadOnlyLookup(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	request := Request{Action: "start", Directory: "/project", Command: "web", RequestID: id}
	encoded, _ := json.Marshal(request)
	const legacy = `{"action":"start","directory":"/project","command":"web","request_id":"11111111-1111-4111-8111-111111111111"}`
	if string(encoded) != legacy {
		t.Fatalf("legacy fingerprint projection changed: %s", encoded)
	}
	s := Store{Data: t.TempDir()}
	if found, e := s.HasRequest(id); e != nil || found {
		t.Fatal(found, e)
	}
	for _, done := range []bool{false, true} {
		path := filepath.Join(s.root(), "receipts", id+".json")
		if e := writePrivate(path, receipt{Fingerprint: "old", Done: done}); e != nil {
			t.Fatal(e)
		}
		if found, e := s.HasRequest(id); e != nil || !found {
			t.Fatal(found, e)
		}
		var read receipt
		if e := tasks.ReadPrivate(path, &read); e != nil || read.Done != done {
			t.Fatal(read, e)
		}
	}
}
