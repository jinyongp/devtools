//go:build !windows

package cli

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
)

func platformCommand(name string) *protocol.Error                       { return nil }
func InternalPlatformPreflight(args []string, diagnostic io.Writer) int { return 0 }
