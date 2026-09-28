package tasks

import (
	"context"
	"fmt"
	"os"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
)

// CacheStamp identifies the active logical journal under the maintenance and
// profile shared locks. A head switch or WAL append invalidates dashboard cache.
func (s Store) CacheStamp(ctx context.Context) (string, *protocol.Error) {
	release, err := maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	if err != nil {
		return "", gateError(ctx)
	}
	defer release()
	profileRelease, err := LockShared(ctx, s.path()+".lock")
	if err != nil {
		return "", gateError(ctx)
	}
	defer profileRelease()

	storage, resolveErr := s.resolveStorage()
	if resolveErr != nil {
		return "", resolveErr
	}
	if storage.Profile.Mode == profilekey.ModeNone {
		return "", nil
	}
	if storage.V3 != nil {
		info, err := os.Lstat(storage.V3.WAL)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", storageError()
		}
		return fmt.Sprintf("v3:%s:%d:%d", storage.V3.Generation, info.ModTime().UnixNano(), info.Size()), nil
	}
	path := storage.Profile.CanonicalPath
	if storage.Profile.Mode == profilekey.ModeLegacy {
		path = storage.Profile.LegacyPath
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", storageError()
	}
	return fmt.Sprintf("v2:%s:%d:%d", path, info.ModTime().UnixNano(), info.Size()), nil
}
