//go:build !windows

package cleanup

import "github.com/jinyongp/devtools/internal/maintenance"

func restoreExternalBackup(path string, body []byte) error { return maintenance.Write(path, body) }
