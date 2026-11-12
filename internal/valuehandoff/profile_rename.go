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
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

// ProfileRenameReadyHeld authenticates unresolved handoffs without opening
// their payloads. Rename holds launch exclusion and the maintenance gate.
func (s Store) ProfileRenameReadyHeld(ctx context.Context, vaultID string, master *vault.Material) *protocol.Error {
	dir := filepath.Join(s.Root, "value-executions", profilekey.Key(s.Profile))
	guard, err := platformfs.OpenRoot(dir, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return values.VaultError(ctx, err)
	}
	defer guard.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return values.VaultError(ctx, err)
	}
	if len(entries) > 0 && master == nil {
		return values.VaultError(ctx, vault.ErrKeyMissing)
	}
	a := &access{vaultID: vaultID, master: master}
	now := time.Now()
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return protocol.ContextError(err)
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !idRE.MatchString(id) || !strings.HasSuffix(entry.Name(), ".json") {
			return values.VaultError(ctx, vault.ErrIntegrity)
		}
		record, err := s.readRecord(ctx, a, id)
		if err != nil {
			return values.VaultError(ctx, err)
		}
		item := record.Item
		resolved := item.ExecutionState == "reported_finished" || item.ExecutionState == "reported_failed"
		switch item.DeliveryState {
		case "prepared", "approved":
			if !now.Before(item.ExpiresAt) && item.DeliveryID == "" {
				continue
			}
		case "canceled", "expired":
			if item.DeliveryID == "" {
				continue
			}
		case "delivered":
			if resolved {
				continue
			}
		}
		return protocol.NewError("handoff_active", "Resolve the pending value execution before renaming the profile.", 3, map[string]any{"execution_id": item.ID})
	}
	return nil
}
