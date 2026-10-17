package cli

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
	"strings"
)

// Removed as native backends become available in W2-W7. In particular, Unix
// permission bits and unsupported locks must never appear to protect a store.
func platformCommand(name string) *protocol.Error {
	if name == "version" {
		return nil
	}
	return protocol.NewError("unsupported_platform", "Windows native features are not implemented in this intermediate build.", 3, nil)
}

// InternalPlatformPreflight also rejects private daemon/helper entry points;
// guarding public commands alone would leave these mutation paths reachable.
func InternalPlatformPreflight(args []string, diagnostic io.Writer) int {
	if len(args) == 0 || !strings.HasPrefix(args[0], "__") {
		return 0
	}
	_ = protocol.Failure(diagnostic, platformCommand("internal"))
	return 3
}
