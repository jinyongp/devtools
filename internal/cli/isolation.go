package cli

import "github.com/jinyongp/devtools/internal/paths"

func (a *App) currentPaths() (paths.Directories, error) {
	if a.isolatedRoot != "" {
		return paths.Isolated(a.isolatedRoot)
	}
	if a.userPaths != nil {
		return a.userPaths()
	}
	return paths.Current()
}
