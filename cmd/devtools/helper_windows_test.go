package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWindowsHelperDispatchRejectsArbitraryTargetsAndCommands(t *testing.T) {
	for _, test := range []struct{ kind, body string }{
		{"shell", `{"command":"calc.exe"}`},
		{"hosts", `{"path":"C:\\outside","expected_digest":"x","mappings":[]}`},
		{"trust", `{"action":"trust","certificate":"","directory":"C:\\outside"}`},
		{"trust", `{"action":"trust","certificate":"","tool":"cmd.exe"}`},
		{"netsetup", `{"command":"arbitrary"}`},
		{"hosts", `{} {}`},
	} {
		failure := windowsHelperOperation(context.Background(), test.kind, json.RawMessage(test.body))
		if failure == nil || failure.Code != "invalid_argument" {
			t.Fatalf("untrusted dispatch %s accepted or misclassified: %v", test.kind, failure)
		}
	}
}
