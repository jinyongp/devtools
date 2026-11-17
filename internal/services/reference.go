package services

import (
	"context"
	"errors"
	"os"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/protocol"
)

// ResolveExecution includes reserved and archived execution IDs. An ended or
// archived ID still prevents the same prefix selecting a different execution.
func (s Store) ResolveExecution(ctx context.Context, input string) (string, *protocol.Error) {
	if idref.UUID.Exact(input) {
		return input, nil
	}
	names, err := s.executionNames()
	if err != nil {
		return "", storageErrorAt("enumerate_execution_ids", err)
	}
	return idref.Resolve(ctx, idref.UUID, input, func(yield func(string) bool) {
		for _, name := range names {
			if !yield(name) {
				return
			}
		}
	})
}

func (s Store) executionNames() ([]string, error) {
	root, err := os.Open(s.root())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Readdirnames(-1)
}
