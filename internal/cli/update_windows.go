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
		failure := windowsinstall.Diagnostic(err, "update")
		failure.Code = "update_failed"
		return nil, failure
	}
	return result, nil
}
