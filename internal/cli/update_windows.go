package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/windowsinstall"
	"strings"
)

func installedExecutableName(name string) bool {
	return strings.EqualFold(name, "devtools.exe") || strings.EqualFold(name, "dvt.exe")
}
func updatePlatform(ctx context.Context, executable string, options map[string]string, streams ...IO) (any, *protocol.Error) {
	result, err := windowsinstall.Update(ctx, executable, options["version"], options["source"])
	if err != nil {
		return nil, protocol.NewError("update_failed", "Cannot update standalone installation; existing user data is preserved. Inspect the installation manifest/journal and retry after releasing its lock; recovery artifacts are retained on failure.", 1, nil)
	}
	return result, nil
}
