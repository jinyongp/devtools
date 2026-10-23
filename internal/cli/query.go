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
		r := remedies[i]
		remedies[i] = protocol.LocalAction(r.Argv, r.RequiredInputs, r.Message, r.Effect).Remedy(protocol.InvocationContext{IsolatedRoot: a.isolatedRoot})
	}
}
