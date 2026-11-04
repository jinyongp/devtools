// Package completion bounds read-only observations used by shell candidate providers.
package completion

import (
	"context"
	"sync"
	"time"
)

const Budget = 200 * time.Millisecond

// Observe isolates slow or unreachable candidates so healthy siblings can still
// complete inside the shell budget. Callers must perform only passive reads.
func Observe[T any](ctx context.Context, input []T, observe func(context.Context, T) (T, bool)) []T {
	ctx, cancel := context.WithTimeout(ctx, 140*time.Millisecond)
	defer cancel()
	jobs, results := make(chan T, len(input)), make(chan T, len(input))
	for _, item := range input {
		jobs <- item
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(8, len(input)) {
		workers.Go(func() {
			for item := range jobs {
				if ctx.Err() != nil {
					return
				}
				probe, release := context.WithTimeout(ctx, 35*time.Millisecond)
				value, ok := observe(probe, item)
				release()
				if ok {
					results <- value
				}
			}
		})
	}
	workers.Wait()
	close(results)
	out := []T{}
	for item := range results {
		out = append(out, item)
	}
	return out
}
