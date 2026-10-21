// A reference same-user receiver. This process belongs to the calling system;
// devtools does not start it, own its jobs or infer its application's health.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"

	"github.com/jinyongp/devtools/internal/valuehandoff"
)

func main() {
	root := flag.String("root", "", "Existing private data root.")
	runner := flag.String("runner-id", "", "Canonical logical runner UUID.")
	instance := flag.String("instance-id", "", "Canonical process instance UUID.")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := valuehandoff.ServeRunner(ctx, *root, *runner, *instance); err != nil && ctx.Err() == nil {
		os.Stderr.WriteString("Runner stopped; inspect execution status before retrying.\n")
		os.Exit(1)
	}
}
