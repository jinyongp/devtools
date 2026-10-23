package values

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
	"path/filepath"
)

// Recover annotates known conditions using the store that observed them. Reading
// the key slot here never starts a session or attempts native authentication.
func Recover(e *protocol.Error, root, profile, env, requestID string, scope protocol.InvocationContext) {
	if e == nil {
		return
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	if profile != "" {
		e.Details["profile"] = profile
	}
	e.Details["env"] = env
	var action protocol.Action
	switch e.Code {
	case "revision_conflict":
		e.Details["retry"] = "after_refresh"
		action = protocol.LocalAction([]string{"devtools", "values", "revision", "--profile", profile}, nil, "Read the current revision, review changed state, and reassess the edit before retrying.", "read")
	case "request_conflict":
		e.Details["retry"] = "inspect_first"
		action = protocol.LocalAction([]string{"devtools", "values", "request", "show", requestID, "--profile", profile}, nil, "Inspect saved request metadata. A lost response requires the original UUID and identical input; a new intent requires a state review and a new UUID.", "read")
	case "vault_locked", "key_interaction_required", "key_backend_locked":
		e.Details["retry"] = "after_dependency_change"
		slot, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Read()
		if err != nil {
			action = protocol.LocalAction([]string{"devtools", "vault", "status"}, nil, "Inspect the same vault protection before choosing its authentication input.", "read")
		} else if slot.Backend == "passphrase" {
			action = protocol.LocalAction([]string{"devtools", "vault", "unlock", "--passphrase-stdin"}, []string{"stdin"}, "Provide the vault passphrase through private stdin (or a private passphrase file), unlock this vault, then retry. Never put it in argv or chat.", "value_write")
		} else {
			action = protocol.LocalAction([]string{"devtools", "vault", "unlock", "--interactive"}, []string{"os-authentication"}, "Explicitly authenticate with this vault's OS protection, then retry.", "value_write")
		}
	case "integrity_failed":
		e.Message = "Cannot authenticate protected data. The passphrase may be incorrect or protected data may be damaged. Preserve the original keys and storage; do not delete or reinitialize them."
		e.Details["retry"] = "inspect_first"
		action = protocol.LocalAction([]string{"devtools", "vault", "status"}, nil, "Inspect the same vault state and verify the authentication input and original data before retrying.", "read")
	default:
		return
	}
	e.Details["remedies"] = []protocol.Remedy{action.Remedy(scope)}
}
