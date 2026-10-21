package valuehandoff

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/vault"
)

// cleanupReplay retires only the authenticated execution selected by a saved
// request. It never refreshes authority or changes the saved request result.
// No timer or read-only inspection decrypts/removes a snapshot while locked.
func (s Store) cleanupReplay(ctx context.Context, a *access, id string) error {
	if !idRE.MatchString(id) {
		return vault.ErrIntegrity
	}
	release, free, err := maintenance.TryLockExclusive(ctx, filepath.Join(s.Root, "value-execution-locks", profilekey.Key(s.Profile), id+".lock"))
	if err != nil || !free {
		return err
	}
	defer release()
	gate, err := s.lockCommit(ctx, a)
	if err != nil {
		return err
	}
	defer gate()
	r, err := s.readRecord(ctx, a, id)
	if err != nil {
		return err
	}
	pending := r.Item.DeliveryState == "prepared" || r.Item.DeliveryState == "approved"
	expired := pending && !time.Now().Before(r.Item.ExpiresAt)
	if pending && !expired {
		return nil
	}
	path := s.path("value-execution-payloads", id)
	_, err = platformfs.StatPrivate(filepath.Join(s.Root, filepath.FromSlash(path)))
	files := map[string][]byte{}
	if err == nil {
		files[path] = nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if expired && r.Item.Revision < math.MaxUint64 {
		r.Item.Revision++
		r.Item.DeliveryState = "expired"
		rp := s.path("value-executions", id)
		files[rp], err = s.encode(ctx, a, rp, id, vault.MaterialExecutionRecord, r)
		if err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return nil
	}
	return maintenance.Replace(s.Root, files)
}
