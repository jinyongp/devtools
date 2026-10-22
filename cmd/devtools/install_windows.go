package main

import (
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/windowsinstall"
	"os"
	"time"
)

func windowsInstallerEntry() bool {
	if len(os.Args) != 2 || os.Args[1] != "__windows-install" {
		return false
	}
	options, err := windowsinstall.DecodeBootstrap(os.Stdin)
	var result *windowsinstall.Result
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		result, err = windowsinstall.Bootstrap(ctx, options)
	}
	if err != nil {
		diagnostic := windowsinstall.Diagnostic(err, "install")
		protocol.Failure(os.Stderr, diagnostic)
		os.Exit(diagnostic.ExitCode)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema_version": 1, "ok": true, "data": result})
	return true
}
