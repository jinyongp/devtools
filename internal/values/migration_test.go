package values

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestLongProfileStateAndManagementReceipts(t *testing.T) {
	for _, n := range []int{122, 123, 125, 126, 128} {
		t.Run(strings.Repeat("n", n), func(t *testing.T) {
			ctx := context.Background()
			s := Store{Directory: filepath.Join(t.TempDir(), "profiles"), Profile: strings.Repeat("A", n)}
			if _, err := s.Update(ctx, func(state *State) (bool, *protocol.Error) {
				return state.Set(Variable, "READY", "", "before")
			}); err != nil {
				t.Fatal(err)
			}
			view, err := s.Inspect(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			value := "after"
			change := Change{Action: "variable.set", Key: "READY", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000001"}
			first, err := s.Apply(ctx, change)
			if err != nil || !first.Changed {
				t.Fatalf("management mutation: %#v %v", first, err)
			}
			replay, err := s.Apply(ctx, change)
			if err != nil || !replay.Replayed || replay.Revision != first.Revision {
				t.Fatalf("receipt replay: %#v %v", replay, err)
			}
			resolved, resolveErr := profilekey.Resolve(s.Directory, s.Profile, "values")
			if resolveErr != nil || resolved.Mode != profilekey.ModeCanonical {
				t.Fatalf("resolution: %#v %v", resolved, resolveErr)
			}
			if len(filepath.Base(s.receiptRelative(change.RequestID))) > 255 {
				t.Fatal("oversized canonical receipt")
			}
			if resolved.LegacyStatus != profilekey.LegacyUnaddressable && !resolved.Marker {
				t.Fatal("missing downgrade blocker")
			}
		})
	}
}

func TestLegacyReceiptReplaysAfterNewerMutation(t *testing.T) {
	ctx := context.Background()
	s := Store{Directory: filepath.Join(t.TempDir(), "profiles"), Profile: "legacy-receipt"}
	view, err := s.Inspect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	value := "original"
	change := Change{Action: "variable.set", Key: "READY", Value: &value, Revision: view.Revision, RequestID: "00000000-0000-4000-8000-000000000002"}
	first, err := s.Apply(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	root := maintenance.Root(s.Directory)
	canonical := filepath.Join(root, s.receiptRelative(change.RequestID))
	legacy := filepath.Join(root, "backup-receipts", "values-"+hex.EncodeToString([]byte(s.Profile))+"-"+change.RequestID+".json")
	if err := os.Rename(canonical, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, func(state *State) (bool, *protocol.Error) {
		return state.Set(Variable, "READY", "", "newer")
	}); err != nil {
		t.Fatal(err)
	}
	replay, err := s.Apply(ctx, change)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatalf("legacy replay: %#v %v", replay, err)
	}
	state, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := state.GetVariable("READY", "")
	if err != nil || current != "newer" {
		t.Fatal("legacy request was executed again")
	}
	// Two disagreeing receipts are storage corruption, even when one matches
	// the requested fingerprint. Do not choose either copy silently.
	body, readErr := os.ReadFile(legacy)
	if readErr != nil {
		t.Fatal(readErr)
	}
	body = []byte(strings.Replace(string(body), `"changed":true`, `"changed":false`, 1))
	if err := maintenance.Write(canonical, body); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, change); err == nil || err.Code != "storage_error" {
		t.Fatalf("conflicting receipts accepted: %v", err)
	}
}
