// The repository-only dashboard runner serves UI source files until interrupted.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jinyongp/devtools/internal/cli"
	"github.com/jinyongp/devtools/internal/dashboard"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/project"
)

func run(ctx context.Context) error {
	// Managed commands launch their supervisor using this executable.
	if len(os.Args) == 4 && os.Args[1] == "__process-serve" {
		return cli.ServeProcess(ctx, os.Args[2], os.Args[3])
	}
	dirs, err := paths.Current()
	if err != nil {
		return err
	}
	p, failure := project.Resolve(".", "")
	if failure != nil {
		return failure
	}
	return dashboard.ServeDevelopment(ctx, filepath.Join(dirs.Data, "tasks"), p.Profile, "internal/dashboard/assets", os.Stdout)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
