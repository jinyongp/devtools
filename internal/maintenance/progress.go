package maintenance

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/progress"
)

func reportLockWait(ctx context.Context, path string) func() {
	message := "Waiting for storage access; another operation holds the lock"
	base := strings.TrimSuffix(filepath.Base(path), ".turnstile")
	if base == ".retained-bodies.lock" {
		message = "Waiting for retained data access; another operation holds the lock"
	} else if filepath.Base(filepath.Dir(path)) == ".storage-upgrade" {
		message = "Waiting for another storage upgrade to finish"
	} else if base == "operations.lock" {
		message = "Waiting for another process operation to finish"
	}
	return progress.Begin(ctx, message)
}
