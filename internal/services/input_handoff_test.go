package services

import (
	"os"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestLauncherPreparationErrorsRemainValueFreeAndActionable(t *testing.T) {
	for _, tc := range []struct{ code, want string }{{"redaction_unavailable", "redaction_unavailable"}, {"vault_locked", "vault_locked"}, {"vault_recovery_required", "vault_recovery_required"}, {"key_interaction_required", "key_interaction_required"}, {"synthetic_sensitive_error_body", "launcher_unavailable"}} {
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			defer write.Close()
			ReportLauncherFailure(write, protocol.NewError(tc.code, "synthetic private message must not be forwarded", 3, nil))
		}()
		channel := &launcherInputChannel{read: read}
		result := channel.expect(launchBound)
		read.Close()
		if result == nil || result.Code != tc.want {
			t.Fatal("private error code not preserved safely", result)
		}
	}
}
