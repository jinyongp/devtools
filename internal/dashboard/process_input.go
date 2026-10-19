package dashboard

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
)

func (s *Server) prepareManagedInput(ctx context.Context, r services.Record, n *network.ExecutionSnapshot) (execution.Prepared, *protocol.Error) {
	return services.PrepareFrozenInput(ctx, r, n, func(command execution.Command) (execution.Dependencies, *protocol.Error) {
		configRoot := ""
		if len(command.Bind)+len(command.Serve) > 0 {
			directories, err := paths.Current()
			if r.IsolatedRoot != "" {
				directories, err = paths.Isolated(r.IsolatedRoot)
			}
			if err != nil {
				return execution.Dependencies{}, protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
			}
			configRoot = directories.Config
		}
		return execution.DependenciesFor(command, filepath.Dir(s.data), configRoot)
	})
}
