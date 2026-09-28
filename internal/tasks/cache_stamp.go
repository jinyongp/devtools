package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
)

// CacheStamp identifies the active physical journal under the maintenance gate.
// A format/path switch invalidates cached projections even if size/time match.
func (s Store) CacheStamp(ctx context.Context) (string, *protocol.Error) {
	release, err := maintenance.AcquireShared(ctx, maintenance.Root(s.Directory))
	if err != nil {
		return "", gateError(ctx)
	}
	defer release()
	resolved, err := profilekey.Resolve(s.Directory, s.Profile, "tasks")
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", storageError()
	}
	if resolved.Mode == profilekey.ModeNone {
		return "", nil
	}
	path := resolved.CanonicalPath
	if resolved.Mode == profilekey.ModeLegacy {
		path = resolved.LegacyPath
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", storageError()
	}
	return fmt.Sprintf("%s:%d:%d", path, info.ModTime().UnixNano(), info.Size()), nil
}
