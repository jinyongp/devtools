package maintenance

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/platformfs"
)

var ErrRuntimeWriterActive = errors.New("runtime storage still has an active writer")

// VerifyRuntimeWritersHeld probes the locks used by already-admitted runtime
// operations. After path closure, older waiters can only re-open the closed
// paths. Live Windows children also retain their inherited process lease.
func VerifyRuntimeWritersHeld(ctx context.Context, root string, staged bool) error {
	paths := make([]string, 0, len(runtimeDomains)+2)
	base := root
	if staged {
		base = filepath.Join(root, VersionedRuntime)
	}
	for _, domain := range runtimeDomains {
		paths = append(paths, filepath.Join(base, domain))
	}
	paths = append(paths, filepath.Join(root, "process-leases"), filepath.Join(root, "exposure-auth"))
	for _, directory := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := platformfs.CheckDirectory(directory); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				return platformfs.CheckDirectory(path)
			}
			if strings.HasSuffix(entry.Name(), ".lock") || strings.HasSuffix(entry.Name(), ".lock.turnstile") {
				return probeRuntimeWriter(ctx, path)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
