package values

import (
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestInputObservationPreservesPendingFailedAndUnknown(t *testing.T) {
	input := &InputVersion{Identity: vault.InputIdentity{Revision: "one"}}
	changed := input.Clone()
	changed.Identity.Revision = "two"
	for _, tc := range []struct {
		name               string
		delivered, current *InputVersion
		code, want         string
	}{
		{"same", input, input, "", "current"}, {"changed", input, changed, "", "pending"}, {"old", nil, input, "", "unknown"}, {"missing current", input, nil, "", "unknown"},
		{"locked", input, nil, "vault_locked", "verification_pending"}, {"old session", input, nil, "key_session_protocol_mismatch", "verification_pending"},
		{"expired", input, nil, "value_expired", "failed"}, {"integrity", input, nil, "integrity_failed", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e *protocol.Error
			if tc.code != "" {
				e = protocol.NewError(tc.code, "synthetic-private-error-must-not-be-copied", 3, nil)
			}
			got := ObserveInput(tc.delivered, tc.current, e)
			if got.State != tc.want {
				t.Fatal(got)
			}
			if got.Delivered != nil {
				got.Delivered.Identity.Revision = "mutated"
			}
			if input.Identity.Revision != "one" {
				t.Fatal("observation aliases input")
			}
		})
	}
}
