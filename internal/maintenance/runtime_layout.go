package maintenance

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
)

const VersionedRuntime = ".runtime-v26"
const RuntimeFenceDocument = "devtools runtime format 26\n"

// RuntimeRelative keeps the canonical data root stable while routing one
// converted domain away from the pathname used by earlier clients.
func RuntimeRelative(root, domain string) string {
	switch domain {
	case "ports", "processes", "project-operations", "network-operations", "proxy", "exposures":
	default:
		return domain
	}
	phase, err := runtimeTransitionPhase(root)
	if err != nil || phase == runtimeClosing || phase == runtimeDraining {
		return path.Join(runtimeTransitionFile, domain)
	}
	if !hasRuntimeFence(root, domain) || RecoveryPointerRelative(root) != fencedRecoveryPointer {
		if phase == runtimeReady {
			return path.Join(runtimeTransitionFile, domain)
		}
		return domain
	}
	return path.Join(VersionedRuntime, domain)
}

func hasRuntimeFence(root, domain string) bool {
	legacy := filepath.Join(root, domain)
	info, err := os.Lstat(legacy)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(RuntimeFenceDocument)) {
		return false
	}
	body, err := Read(legacy, int64(len(RuntimeFenceDocument)))
	return err == nil && bytes.Equal(body, []byte(RuntimeFenceDocument))
}

func RuntimeDirectory(root, domain string) string {
	return filepath.Join(root, filepath.FromSlash(RuntimeRelative(root, domain)))
}
