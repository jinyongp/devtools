package maintenance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtectedRootSurvivesOrdinaryTransactionRecovery(t *testing.T) {
	for _, phase := range []string{"prepared", "partial", "committed", "complete"} {
		t.Run(phase, func(t *testing.T) {
			root := privateTempDir(t)
			ctx := context.Background()
			release, err := AcquireExclusive(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			id := strings.Repeat("a", 32)
			if err := writeProtectedRoot(root, id); err != nil {
				t.Fatal(err)
			}
			path := "tasks/61.json"
			if err := Write(filepath.Join(root, path), []byte("before")); err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{path: []byte("after")}
			if phase == "complete" {
				if err := Replace(root, files); err != nil {
					t.Fatal(err)
				}
			} else {
				p, err := prepareTransaction(root, files)
				if err != nil {
					t.Fatal(err)
				}
				if p.Version != 4 || p.VaultID != id {
					t.Fatal("transaction dropped protected-root version")
				}
				if phase != "prepared" {
					if err := applyReplacement(root, files); err != nil {
						t.Fatal(err)
					}
				}
				if phase == "committed" {
					if ok, err := markCommitted(root, p); !ok || err != nil {
						t.Fatal(err)
					}
				}
			}
			release()
			release, err = AcquireShared(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			got, protected, err := ProtectedRootHeld(root)
			if err != nil || !protected || got != id {
				t.Fatal("format barrier lost", err)
			}
			b, err := Read(filepath.Join(root, path), 1024)
			if err != nil {
				t.Fatal(err)
			}
			want := "before"
			if phase == "committed" || phase == "complete" {
				want = "after"
			}
			if string(b) != want {
				t.Fatal("incorrect recovery outcome")
			}
			// The pre-migration reader accepts only v2 pointers or a legacy array.
			b, err = Read(pendingPath(root), 1024)
			if err != nil {
				t.Fatal(err)
			}
			var legacy struct {
				Version int `json:"version"`
			}
			if json.Unmarshal(b, &legacy) != nil || legacy.Version == 2 {
				t.Fatal("old reader could accept storage")
			}
		})
	}
}
