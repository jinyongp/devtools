package execution

import (
	"errors"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
)

func OutputProtectionFailure(err error, scope protocol.InvocationContext) *protocol.Error {
	facts := map[string]any{}
	var limit *redact.LimitError
	if errors.As(err, &limit) && limit.Valid() {
		facts["limit_kind"] = limit.Kind
		facts["observed"] = limit.Observed
		facts["limit"] = limit.Limit
	}
	remedies := []protocol.Remedy{{Argv: []string{}, RequiredInputs: []string{}, Effect: "config_write", Message: "Keep output protection enabled. Select a smaller bundle containing only values required by this execution. Matcher limits are fixed; raw output is not a recovery default."}}
	remedies = append([]protocol.Remedy{protocol.LocalAction([]string{"devtools", "bundle", "list"}, []string{"profile"}, "Inspect existing bundles before narrowing the selected values.", "read").Remedy(scope)}, remedies...)
	return protocol.NewError("redaction_unavailable", "Cannot initialize output protection. Limit facts report normalized matcher counts at the rejection point, not the total number of input secrets.", 3, map[string]any{"phase": "preflight", "facts": facts, "remedies": remedies, "retry": "after_input_change"})
}
