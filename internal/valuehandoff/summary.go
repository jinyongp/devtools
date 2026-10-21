package valuehandoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/vault"
)

type Summary struct {
	Verification    string `json:"verification"`
	Total           *int   `json:"total,omitempty"`
	PendingInputs   *int   `json:"pending_inputs,omitempty"`
	AttemptedInputs *int   `json:"attempted_inputs,omitempty"`
}

// InspectSummary is passive and aggregate only. It authenticates every record,
// including records whose source profile no longer exists. No partial count is
// published when keys or a complete verified snapshot are unavailable.
func InspectSummary(ctx context.Context, root string) Summary {
	pending := Summary{Verification: "verification_pending"}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		return pending
	}
	root = canonical
	s := Store{Root: root, Profile: "runners"}
	a, err := s.access(ctx, false)
	if err != nil {
		return pending
	}
	defer a.master.Destroy()
	gate, err := maintenance.AcquireShared(ctx, root)
	if err != nil {
		return pending
	}
	defer gate()
	dir := filepath.Join(root, "value-executions")
	guard, err := platformfs.OpenRoot(dir, false)
	total, waiting, attempted := 0, 0, 0
	if errors.Is(err, os.ErrNotExist) {
		return Summary{"verified", &total, &waiting, &attempted}
	}
	if err != nil {
		return pending
	}
	defer guard.Close()
	profiles, err := os.ReadDir(dir)
	if err != nil {
		return pending
	}
	for _, profile := range profiles {
		if !profile.IsDir() {
			return pending
		}
		profileDir := filepath.Join(dir, profile.Name())
		g, err := platformfs.OpenRoot(profileDir, false)
		if err != nil {
			return pending
		}
		entries, err := os.ReadDir(profileDir)
		g.Close()
		if err != nil {
			return pending
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return pending
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if !strings.HasSuffix(entry.Name(), ".json") || !idRE.MatchString(id) {
				return pending
			}
			path := "value-executions/" + profile.Name() + "/" + entry.Name()
			b, err := s.openBound(ctx, a, path, id, vault.MaterialExecutionRecord)
			if err != nil {
				return pending
			}
			var r record
			err = vault.StrictDecode(b.Data, &r)
			clear(b.Data)
			if err != nil || !project.ValidProfile(b.Profile) || profilekey.Key(b.Profile) != profile.Name() || r.Ticket.Profile != b.Profile || r.Item.ID != id || !idRE.MatchString(r.Item.InputID) || r.Item.Revision == 0 || r.Item.ExpiresAt.IsZero() {
				return pending
			}
			total++
			if r.Item.DeliveryID != "" {
				attempted++
			} else if (r.Item.DeliveryState == "prepared" || r.Item.DeliveryState == "approved") && time.Now().Before(r.Item.ExpiresAt) {
				waiting++
			}
		}
	}
	return Summary{"verified", &total, &waiting, &attempted}
}
