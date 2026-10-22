package services

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/tasks"
)

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
