package maintenance

import (
	"context"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func stageRuntimeDirectory(ctx context.Context, handle *platformfs.Root, _, from, to string) error {
	return handle.PublishDirectory(ctx, from, to)
}
