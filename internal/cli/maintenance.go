package cli

import (
	"context"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/cleanup"
)

func (a *App) maintainAfterWrite(ctx context.Context, command string, r Request) {
	related := map[string]bool{"process start": true, "process stop": true, "process restart": true, "project up": true, "project down": true, "project restart": true, "expose start": true, "expose stop": true, "expose auth": true, "values execution prepare": true, "values execution approve": true, "values execution cancel": true, "values execution deliver": true, "values execution report": true}[command]
	if !related {
		return
	}
	dirs, err := a.currentPaths()
	if err != nil {
		return
	}
	dir, condition := a.dataDirectory()
	if condition != nil {
		return
	}
	if filepath.Clean(dirs.Data) != filepath.Clean(filepath.Dir(dir)) {
		dirs.Cache = ""
	}
	bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	// The initiating write has already committed. A delayed maintenance pass is
	// retried at its saved position after a later related write or explicitly.
	_, _ = (cleanup.Engine{Data: filepath.Dir(dir), Cache: dirs.Cache, Config: dirs.Config}).Maintain(bounded, r.Options["profile"], 32, false)
}
