//go:build !windows

package process

import (
	"debug/pe"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func runningInWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	release, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(release)), "microsoft")
}
func ValidateManaged(args []string, dir string, env []string) *protocol.Error {
	if len(args) == 0 || !runningInWSL() {
		return nil
	}
	path, err := LookPath(args[0], dir, env)
	if err != nil {
		return nil
	}
	path, resolveErr := filepath.EvalSymlinks(path)
	if resolveErr != nil {
		return nil
	}
	f, openErr := platformfs.OpenNoFollow(path, os.O_RDONLY, 0)
	if openErr != nil {
		return nil
	}
	defer f.Close()
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return nil
	}
	image, parseErr := pe.NewFile(f)
	if parseErr == nil && image.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE != 0 && image.Characteristics&pe.IMAGE_FILE_DLL == 0 {
		return protocol.NewError("unsupported_platform", "Manage Windows executables with native Windows devtools; a WSL supervisor cannot own the cross-OS process tree.", 3, nil)
	}
	return nil
}
