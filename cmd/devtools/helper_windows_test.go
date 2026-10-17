package main

import (
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "__windows-helper-lifetime-test" {
		ctx, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer stop()
		runWindowsHelperWithLifetime(ctx, func() *protocol.Error { select {} })
		os.Exit(91)
	}
	os.Exit(m.Run())
}

func TestWindowsHelperLifetimeBoundsBlockedOperation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	child := exec.CommandContext(ctx, exe, "__windows-helper-lifetime-test")
	err = child.Run()
	if err == nil || ctx.Err() != nil || child.ProcessState == nil || child.ProcessState.ExitCode() != 130 {
		t.Fatalf("blocked one-shot helper did not end at deadline: %v", err)
	}
}

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
