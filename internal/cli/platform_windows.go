package cli

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
	"strings"
)

func platformCommand(string) *protocol.Error { return nil }

// Native implementations validate their own ownership and input. Unix-only or
// unknown private entry points remain unavailable before any store is opened.
func InternalPlatformPreflight(args []string, diagnostic io.Writer) int {
	if len(args) == 0 || !strings.HasPrefix(args[0], "__") {
		return 0
	}
	switch args[0] {
	case "__complete", "__trust-check", "__exposure-provider", "__proxy-serve", "__process-serve", "__process-launch", "__dashboard-serve":
		return 0
	}
	_ = protocol.Failure(diagnostic, protocol.NewError("unsupported_platform", "This private entry point is unavailable on Windows.", 3, nil))
	return 3
}
