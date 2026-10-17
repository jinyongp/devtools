package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jinyongp/devtools/internal/cli"
	"github.com/jinyongp/devtools/internal/dashboard"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netsetup"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/truststore"
	"golang.org/x/term"
)

var version = "dev"
var commit = "unknown"

func main() {
	if code := cli.InternalPlatformPreflight(os.Args[1:], os.Stderr); code != 0 {
		os.Exit(code)
	}
	if len(os.Args) == 2 && os.Args[1] == "__trust-check" {
		data, e := io.ReadAll(io.LimitReader(os.Stdin, 65536))
		if e != nil {
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(truststore.CheckPublic(data))
		return
	}
	if (len(os.Args) == 4 || len(os.Args) == 5) && os.Args[1] == "__exposure-provider" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		os.Exit(expose.RunCloudflaredSupervisor(ctx, os.Args[2], os.Args[3], os.Stdin, os.Stdout, os.Stderr, os.Args[4:]...))
	}
	if len(os.Args) == 3 && (os.Args[1] == "__install-prepare" || os.Args[1] == "__install-commit" || os.Args[1] == "__install-rollback") {
		d, e := paths.Current()
		if e != nil || d.IsolatedRoot != "" {
			os.Exit(3)
		}
		m := netsetup.Manager{Data: d.Data, Terminal: term.IsTerminal(int(os.Stderr.Fd()))}
		if os.Args[1] == "__install-prepare" {
			if m.PrepareReplacement(context.Background(), os.Args[2]) != nil {
				os.Exit(3)
			}
		} else if m.CommitReplacement(os.Args[2], os.Args[1] == "__install-rollback") != nil {
			os.Exit(3)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "__network-helper" && os.Args[2] == "netsetup" {
		if os.Geteuid() != 0 || os.Getenv("DEVTOOLS_ISOLATED_ROOT") != "" {
			os.Exit(3)
		}
		var request netsetup.Request
		decoder := json.NewDecoder(io.LimitReader(os.Stdin, 65536))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			os.Exit(2)
		}
		executable, e := os.Executable()
		if e != nil || (netsetup.SystemApply(context.Background(), request, executable)) != nil {
			os.Exit(3)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "__network-helper" && os.Args[2] == "hosts" {
		if os.Geteuid() != 0 || os.Getenv("DEVTOOLS_ISOLATED_ROOT") != "" {
			os.Exit(3)
		}
		var request hosts.Request
		decoder := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			os.Exit(2)
		}
		if (hosts.System{}).Apply(context.Background(), request) != nil {
			os.Exit(3)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "__network-helper" && os.Args[2] == "trust" {
		if os.Geteuid() != 0 || os.Getenv("DEVTOOLS_ISOLATED_ROOT") != "" {
			os.Exit(3)
		}
		var request truststore.Request
		decoder := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			os.Exit(2)
		}
		if (truststore.System{}).Apply(context.Background(), request) != nil {
			os.Exit(3)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "__proxy-serve" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		if (proxy.Manager{Data: os.Args[2]}).Serve(ctx, os.Args[3]) != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 5 && os.Args[1] == "__process-serve" && os.Args[4] == "3" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		lease := os.NewFile(3, "process-lease")
		if lease == nil {
			os.Exit(1)
		}
		defer lease.Close()
		if cli.ServeProcessWithLease(ctx, os.Args[2], os.Args[3], lease) != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "__process-serve" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		if cli.ServeProcess(ctx, os.Args[2], os.Args[3]) != nil {
			os.Exit(1)
		}
		return
	}
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
