// A reference same-user receiver. This process belongs to the calling system;
// devtools does not start it, own its jobs or infer its application's health.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"

	"github.com/jinyongp/devtools/internal/runnerengine"
	"github.com/jinyongp/devtools/internal/valuehandoff"
)

func main() {
	root := flag.String("root", "", "Existing private data root.")
	runner := flag.String("runner-id", "", "Canonical logical runner UUID.")
	instance := flag.String("instance-id", "", "Canonical process instance UUID.")
	ledger := flag.String("ledger", "", "Caller-owned private metadata ledger directory, outside the devtools root.")
	program := flag.String("program", "", "Absolute helper executable; selected values are supplied only through its environment.")
	lookup := flag.String("lookup-job", "", "Read a durable engine result without starting a receiver or job.")
	initialize := flag.Bool("init-ledger", false, "Explicitly initialize a new caller ledger, then exit. Never use to recover lost history.")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	engine := &runnerengine.Engine{Directory: *ledger, Program: *program, Arguments: flag.Args()}
	if *initialize {
		if err := engine.Initialize(ctx); err != nil {
			os.Stderr.WriteString("Cannot initialize private engine ledger.\n")
			os.Exit(3)
		}
		return
	}
	if *lookup != "" {
		result, err := engine.Lookup(ctx, *lookup)
		if err != nil {
			os.Stderr.WriteString("Engine outcome unavailable; do not resubmit.\n")
			os.Exit(3)
		}
		if json.NewEncoder(os.Stdout).Encode(result) != nil {
			os.Exit(1)
		}
		return
	}
	receiver := valuehandoff.Receiver{Root: *root, RunnerID: *runner, InstanceID: *instance, Stdout: os.Stdout, Stderr: os.Stderr, Accept: engine.Accept, Handle: engine.Handle}
	if err := receiver.Serve(ctx); err != nil && ctx.Err() == nil {
		os.Stderr.WriteString("Runner stopped; inspect execution status before retrying.\n")
		os.Exit(1)
	}
}
