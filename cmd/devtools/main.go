package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/jinyongp/devtools/internal/cli"
	"github.com/jinyongp/devtools/internal/dashboard"
	"github.com/jinyongp/devtools/internal/process"
)

var version = "dev"
var commit = "unknown"

func main() {
	if len(os.Args) == 4 && os.Args[1] == "__dashboard-serve" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if dashboard.Serve(ctx, os.Args[2], os.Args[3]) != nil {
			os.Exit(1)
		}
		return
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case sig := <-signals:
			cancel(process.SignalError{Signal: sig.(syscall.Signal)})
		case <-ctx.Done():
		}
	}()
	code := cli.New(version, commit).Run(ctx, os.Args[1:], cli.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
	signal.Stop(signals)
	cancel(nil)
	os.Exit(code)
}
