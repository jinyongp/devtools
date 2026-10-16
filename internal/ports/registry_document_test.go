package ports

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestRegistryMigrationNormalizesAssignmentsAndJoinsMetadata(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			s := testPortStore(t)
			if err := os.MkdirAll(s.Directory, 0700); err != nil {
				t.Fatal(err)
			}
			i := Instance{Profile: "test", ID: "0123456789abcdef0123456789abcdef", Directory: t.TempDir()}
			legacy := empty()
			legacy.Version = version
			legacy.Instances = []Instance{i}
			legacy.Assignments = []Assignment{{Instance: i, Name: "web", Port: 32100}, {Instance: i, Name: "api", Port: 32101}}
			if version == 3 {
				legacy.UpgradeNetwork()
			}
			path := filepath.Join(s.Directory, "registry.json")
			original, _ := json.Marshal(legacy)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.Update(context.Background(), func(st *State) (bool, *protocol.Error) { return false, nil }); err != nil {
				t.Fatal(err)
			}
			unchanged, _ := os.ReadFile(path)
			if string(unchanged) != string(original) {
				t.Fatal("no-op migrated registry")
			}
			if err := s.Update(context.Background(), func(st *State) (bool, *protocol.Error) {
				alias := "renamed"
				st.ByID(i.ID).Alias = &alias
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			body, _ := os.ReadFile(path)
			var stored struct {
				Version     int              `json:"version"`
				Assignments []map[string]any `json:"assignments"`
			}
			if json.Unmarshal(body, &stored) != nil || stored.Version != registryVersion || len(stored.Assignments) != 2 {
				t.Fatal(string(body))
			}
			for _, a := range stored.Assignments {
				if len(a) != 3 || a["instance_id"] != i.ID {
					t.Fatal("assignment duplicates owner metadata", a)
				}
			}
			st, err := s.Read()
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"web", "api"} {
				a := st.Get(i.ID, name)
				if a == nil || a.Alias == nil || *a.Alias != "renamed" || a.Profile != i.Profile || a.Directory != i.Directory {
					t.Fatal(a)
				}
			}
			// Mutations receive no cached slice positions from the preceding read.
			if err := s.Update(context.Background(), func(st *State) (bool, *protocol.Error) {
				st.Assignments = st.Assignments[1:]
				if st.Get(i.ID, "web") != nil || st.Get(i.ID, "api") == nil {
					t.Fatal("stale assignment index")
				}
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRegistrySizeLimitPreservesCommittedDocument(t *testing.T) {
	s := testPortStore(t)
	if _, _, err := s.Reserve(context.Background(), "proxy", 32100); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Directory, "registry.json")
	before, _ := os.ReadFile(path)
	err := s.Update(context.Background(), func(st *State) (bool, *protocol.Error) {
		st.Instances = append(st.Instances, Instance{ID: "0123456789abcdef0123456789abcdef", Profile: "test", Directory: "/" + strings.Repeat("d", registryDocumentLimit)})
		return true, nil
	})
	if err == nil || err.Code != "invalid_storage" {
		t.Fatal("unreadable result committed", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("size failure replaced committed document")
	}
	// A valid JSON prefix must not hide bytes past the reader's limit.
	oversize := append(append([]byte{}, before...), []byte(strings.Repeat(" ", registryDocumentLimit))...)
	if err := os.WriteFile(path, oversize, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(); err == nil || err.Code != "invalid_storage" {
		t.Fatal("oversized document accepted", err)
	}
}

func TestRegistryRejectsInconsistentLegacyAndMalformedNormalizedRelations(t *testing.T) {
	i := Instance{Profile: "test", ID: "0123456789abcdef0123456789abcdef", Directory: t.TempDir()}
	for _, test := range []struct {
		name       string
		version    int
		assignment any
	}{
		{"legacy_mismatch", 2, Assignment{Instance: Instance{ID: i.ID, Profile: "other", Directory: i.Directory}, Name: "web", Port: 32100}},
		{"orphan", 4, storedAssignment{InstanceID: "missing", Name: "web", Port: 32100}},
		{"duplicate_metadata", 4, Assignment{Instance: i, Name: "web", Port: 32100}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testPortStore(t)
			os.MkdirAll(s.Directory, 0700)
			body, _ := json.Marshal(map[string]any{"version": test.version, "instances": []Instance{i}, "assignments": []any{test.assignment}, "reservations": []Reservation{}})
			if err := os.WriteFile(filepath.Join(s.Directory, "registry.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Read(); err == nil || err.Code != "invalid_storage" {
				t.Fatal("corrupt relation accepted", err)
			}
		})
	}
}
