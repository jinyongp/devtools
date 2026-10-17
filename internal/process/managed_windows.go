package process

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"path/filepath"
	"strings"
)

// ValidateManaged rejects a direct OS bridge before a managed process starts.
// It does not try to inspect arbitrary scripts or predict their descendants.
func ValidateManaged(args []string, dir string, env []string) *protocol.Error {
	if len(args) == 0 {
		return nil
	}
	path, err := LookPath(args[0], dir, env)
	if err != nil {
		return nil
	} // ordinary diagnostics own missing tools
	if strings.EqualFold(strings.TrimRight(filepath.Base(path), " ."), "wsl.exe") {
		return protocol.NewError("unsupported_platform", "Manage WSL commands with the Linux devtools inside that WSL distribution; a native Windows supervisor cannot own the cross-OS process tree.", 3, nil)
	}
	return nil
}
