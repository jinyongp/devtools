package values

import "github.com/jinyongp/devtools/internal/protocol"

// InputObservation compares recorded delivery to a freshly inspected goal. It
// neither observes application reads nor proves an external credential works.
type InputObservation struct {
	State     string        `json:"state"`
	Reason    string        `json:"reason"`
	Delivered *InputVersion `json:"delivered"`
	Current   *InputVersion `json:"current"`
}

func ObserveInput(delivered, current *InputVersion, inspectionError *protocol.Error) InputObservation {
	out := InputObservation{State: "unknown", Reason: "delivery_unknown", Delivered: delivered.Clone(), Current: current.Clone()}
	if inspectionError != nil {
		out.State = "failed"
		out.Reason = inspectionError.Code
		switch inspectionError.Code {
		case "vault_locked", "lock_pending", "key_missing", "key_backend_unavailable", "key_access_denied", "key_interaction_required", "key_backend_locked", "key_session_protocol_mismatch", "vault_recovery_required":
			out.State = "verification_pending"
		}
		return out
	}
	if same, known := SameInput(delivered, current); known {
		if same {
			out.State = "current"
			out.Reason = "same_effective_input"
		} else {
			out.State = "pending"
			out.Reason = "effective_input_changed"
		}
	}
	return out
}
