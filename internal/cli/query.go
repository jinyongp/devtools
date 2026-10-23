package cli

import "github.com/jinyongp/devtools/internal/protocol"

// Keep recovery instructions on the caller's store and selected environment.
func (a *App) scopeQueryRemedy(failure *protocol.Error, env string) {
	if failure.Details["state"] == nil {
		return
	}
	remedies, _ := failure.Details["remedies"].([]protocol.Remedy)
	for i := range remedies {
		if env != "" {
			remedies[i].Argv = append(remedies[i].Argv, "--env", env)
		}
		if a.isolatedRoot != "" {
			remedies[i].Argv = append(remedies[i].Argv, "--isolated-root", a.isolatedRoot)
		}
	}
}
