package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWindowsPublicPlatformAndPrivateAllowlist(t *testing.T) {
	for _, name := range []string{"variable set", "process start", "project up", "proxy start", "completion", "diagnostics", "update"} {
		if failure := platformCommand(name); failure != nil {
			t.Fatal(name, failure)
		}
	}
	for _, name := range []string{"__complete", "__trust-check", "__exposure-provider", "__proxy-serve", "__process-serve", "__process-launch", "__dashboard-serve"} {
		var output bytes.Buffer
		if code := InternalPlatformPreflight([]string{name}, &output); code != 0 || output.Len() != 0 {
			t.Fatal(name, code, output.String())
		}
	}
	for _, name := range []string{"__network-helper", "__install-prepare", "__install-commit", "__install-rollback", "__unknown"} {
		var output bytes.Buffer
		if code := InternalPlatformPreflight([]string{name}, &output); code != 3 || !strings.Contains(output.String(), `"code":"unsupported_platform"`) {
			t.Fatal(name, code, output.String())
		}
	}
}
