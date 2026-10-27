package valuehandoff

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/retention"
	"github.com/jinyongp/devtools/internal/vault"
)

// InspectRetentionHeld authenticates metadata only; payload bytes are measured
// through private file handles and are never decrypted by an inventory request.
func InspectRetentionHeld(ctx context.Context, inspection *vault.Inspection, profile string, out *retention.Inventory) error {
	s := Store{Root: inspection.Root, Profile: "runners"}
	a := &access{master: inspection.Master, vaultID: inspection.VaultID}
	records := map[string]record{}
	for _, domain := range []string{"value-executions", "value-execution-requests", "value-execution-payloads"} {
		dir := filepath.Join(s.Root, domain)
		guard, err := platformfs.OpenRoot(dir, false)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		profiles, err := os.ReadDir(dir)
		guard.Close()
		if err != nil {
			return err
		}
		for _, p := range profiles {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !p.IsDir() {
				return vault.ErrInvalid
			}
			pd := filepath.Join(dir, p.Name())
			guard, err := platformfs.OpenRoot(pd, false)
			if err != nil {
				return err
			}
			entries, err := os.ReadDir(pd)
			guard.Close()
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				id := strings.TrimSuffix(entry.Name(), ".json")
				if !strings.HasSuffix(entry.Name(), ".json") || domain == "value-execution-requests" && !uuidRE.MatchString(id) || domain != "value-execution-requests" && !idRE.MatchString(id) {
					return vault.ErrInvalid
				}
				info, err := platformfs.StatPrivate(filepath.Join(pd, entry.Name()))
				if err != nil || !info.Mode().IsRegular() {
					return vault.ErrInvalid
				}
				path := domain + "/" + p.Name() + "/" + entry.Name()
				state, kind, reason, verification := "", "", "", "authenticated"
				var owner string
				if domain == "value-execution-payloads" {
					r, ok := records[p.Name()+"/"+id]
					if !ok {
						return vault.ErrIntegrity
					}
					owner = r.Ticket.Profile
					kind = "execution_payload"
					verification = "record_verified"
					state = "live"
					reason = "active_input"
					if !time.Now().Before(r.Item.ExpiresAt) {
						state = "expired"
						reason = "retire_on_authenticated_mutation"
					}
					if r.Item.DeliveryID != "" {
						state = "attempted"
						reason = "retain_uncertain_delivery"
					}
				} else {
					purpose := vault.MaterialExecutionRecord
					if domain == "value-execution-requests" {
						purpose = vault.MaterialExecutionRequest
					}
					b, err := s.openBound(ctx, a, path, id, purpose)
					if err != nil {
						return err
					}
					owner = b.Profile
					if !project.ValidProfile(owner) || profilekey.Key(owner) != p.Name() {
						clear(b.Data)
						return vault.ErrIntegrity
					}
					if domain == "value-executions" {
						var r record
						err = vault.StrictDecode(b.Data, &r)
						clear(b.Data)
						if err != nil || r.Ticket.Profile != owner || r.Item.ID != id || !idRE.MatchString(r.Item.InputID) || r.Item.Revision == 0 || r.Item.ExpiresAt.IsZero() {
							return vault.ErrIntegrity
						}
						records[p.Name()+"/"+id] = r
						kind = "execution_record"
						state = r.Item.DeliveryState
						reason = "retain_execution_history"
						if (state == "prepared" || state == "approved") && !time.Now().Before(r.Item.ExpiresAt) {
							state = "expired"
						}
					} else {
						var r receipt
						err = vault.StrictDecode(b.Data, &r)
						clear(b.Data)
						if err != nil || len(r.Fingerprint) != 64 || r.Result.Profile != owner || r.ExpiresAt.IsZero() {
							return vault.ErrIntegrity
						}
						kind = "execution_receipt"
						state = "replay_window"
						reason = "retain_request_identity"
						if !time.Now().Before(r.ExpiresAt) {
							state = "spent"
						}
						if r.Pending || r.ErrorCode == "delivery_outcome_unknown" {
							state = "unknown"
							reason = "retain_uncertain_delivery"
						}
					}
				}
				if profile != "" && owner != profile {
					continue
				}
				out.Add(kind, state, "profile", verification, reason, owner+":"+id, false, info.Size())
			}
		}
	}
	return nil
}
