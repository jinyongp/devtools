package services

import (
	"context"

	"io"

	"os"

	"sync"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Execute func(context.Context, Record, process.Runner, io.Writer) *protocol.Error

type environmentSelectionKey struct{}
type environmentSelection struct {
	Env    string
	Inject bool
}

// WithEnvironmentSelection carries the actual prepared selection to the
// supervisor, which records it only when the child starts successfully.
func WithEnvironmentSelection(ctx context.Context, env string, inject bool) context.Context {
	return context.WithValue(ctx, environmentSelectionKey{}, environmentSelection{Env: env, Inject: inject})
}

// Serve supports markerless legacy/internal invocations by acquiring the lease
// itself. New launch protocol records must use ServeWithLease.
func (s Store) Serve(ctx context.Context, id string, execute Execute) error {
	return s.serve(ctx, id, nil, execute)
}

func (s Store) ServeWithLease(ctx context.Context, id string, leaseFile *os.File, execute Execute) error {
	return s.serve(ctx, id, leaseFile, execute)
}

// stdout and stderr share the in-process mutex so each child Write is kept as
// one logical record. Cross-process readers and cleanup synchronize through the
// bounded log store's advisory lock.
type boundedLog struct {
	mu    sync.Mutex
	path  string
	stats *logIOStats
}

func (l *boundedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return (boundedLogStore{path: l.path, stats: l.stats}).write(p)
}
