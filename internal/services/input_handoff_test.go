package services

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
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

func TestLauncherLimitFrameIsClosedAndBounded(t *testing.T) {
	condition := execution.OutputProtectionFailure(&redact.LimitError{Kind: "patterns", Observed: redact.MaxPatterns + 1, Limit: redact.MaxPatterns}, protocol.InvocationContext{})
	condition.Message = "PRIVATE-CANARY"
	condition.Details["raw"] = "PRIVATE-CANARY"
	var encoded bytes.Buffer
	ReportLauncherFailure(&encoded, condition)
	if encoded.Len() != 18 || bytes.Contains(encoded.Bytes(), []byte("PRIVATE-CANARY")) {
		t.Fatal(encoded.Bytes())
	}
	valid := append([]byte{}, encoded.Bytes()...)
	invalidEnum := append([]byte{}, valid...)
	invalidEnum[1] = 255
	invalidCount := append([]byte{}, valid...)
	binary.BigEndian.PutUint64(invalidCount[2:10], ^uint64(0))
	for _, tc := range []struct {
		frame []byte
		want  string
	}{{valid, "redaction_unavailable"}, {valid[:5], "launcher_unavailable"}, {invalidEnum, "launcher_unavailable"}, {invalidCount, "launcher_unavailable"}, {[]byte{255}, "launcher_unavailable"}} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		go func() { defer w.Close(); w.Write(tc.frame) }()
		channel := &launcherInputChannel{read: r}
		result := channel.expect(launchBound)
		r.Close()
		if result == nil || result.Code != tc.want {
			t.Fatal(result)
		}
		if tc.want == "redaction_unavailable" && result.Details["facts"].(map[string]any)["observed"] != redact.MaxPatterns+1 {
			t.Fatal(result)
		}
	}
}
