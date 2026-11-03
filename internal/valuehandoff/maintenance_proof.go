package valuehandoff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/vault"
)

// A bounded pass accumulates authenticated evidence of newer accepted work.
// Later passes can retire an older failure without decoding every execution.
// The compact proof remains after detail retirement, like the current summary.
func (s Store) maintainResolution(ctx context.Context, a *access, item Execution, dry bool) (bool, error) {
	hash := sha256.Sum256([]byte(s.Profile + "\x00" + item.ExternalExecutionID))
	name := hex.EncodeToString(hash[:])
	path, id := ".retention/delivery-latest/"+name+".json", name[:32]
	var proof struct {
		Version int       `json:"version"`
		Item    Execution `json:"item"`
	}
	err := s.decode(ctx, a, path, id, vault.MaterialExecutionRecord, &proof)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil && (proof.Version != 1 || proof.Item.ExternalExecutionID != item.ExternalExecutionID) {
		return false, vault.ErrIntegrity
	}
	newer := proof.Item.ID != "" && (proof.Item.CreatedAt.After(item.CreatedAt) || proof.Item.CreatedAt.Equal(item.CreatedAt) && proof.Item.ID > item.ID)
	accepted := item.DeliveryID != "" || item.ExecutionState == "reported_started" || item.ExecutionState == "reported_finished" || item.ExecutionState == "reported_failed"
	if !newer && accepted && proof.Item.ID != item.ID && !dry {
		proof.Version, proof.Item = 1, item
		gate, err := s.lockCommit(ctx, a)
		if err != nil {
			return false, err
		}
		defer gate()
		body, err := s.encode(ctx, a, path, id, vault.MaterialExecutionRecord, proof)
		if err != nil {
			return false, err
		}
		defer clear(body)
		if err := maintenance.Write(filepath.Join(s.Root, filepath.FromSlash(path)), body); err != nil {
			return false, err
		}
	}
	return newer, nil
}
