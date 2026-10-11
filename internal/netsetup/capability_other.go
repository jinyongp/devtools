//go:build !linux

package netsetup

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
)

func capability(*os.File) (bool, *protocol.Error)  { return false, nil }
func setCapability(*os.File, bool) *protocol.Error { return failure("unsupported_platform") }
