package process

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsManagedWslBridgeRejectedForegroundAllowed(t *testing.T) {
	dir, exe := windowsChild(t)
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	bridge := filepath.Join(dir, "wsl.exe")
	if err = os.WriteFile(bridge, body, 0700); err != nil {
		t.Fatal(err)
	}
	env := Environment(os.Environ(), map[string]string{"DEVTOOLS_ARGV_CHILD": "streams", "PATH": dir})
	if failure := ValidateManaged([]string{"wsl"}, dir, env); failure == nil || failure.Code != "unsupported_platform" {
		t.Fatalf("managed direct OS bridge accepted: %v", failure)
	}
	if failure := ValidateManaged([]string{exe}, dir, env); failure != nil {
		t.Fatalf("native child rejected: %v", failure)
	}
	code, failure := Execute(context.Background(), []string{bridge, "-test.run=^TestWindowsArgvChild$"}, dir, env, nil, io.Discard, io.Discard)
	if failure != nil || code != 27 {
		t.Fatalf("foreground OS bridge was blocked: %d %v", code, failure)
	}
}
