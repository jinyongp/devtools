package cleanup

import (
	"context"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func removeAndSync(path string) error {
	return platformfs.RemoveExternalPrivate(context.Background(), path)
}
