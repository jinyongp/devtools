package windowsinstall

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCompletionRepairsMissingRegistrationAndScript(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "completion")
	documents := filepath.Join(t.TempDir(), "한글 Documents")
	profile := filepath.Join(documents, "WindowsPowerShell", "profile.ps1")
	if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
		t.Fatal(err)
	}
	original := utf16Profile("# existing user settings\r\n", binary.LittleEndian)
	if err := os.WriteFile(profile, original, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := publishPowerShellCompletion(ctx, directory, documents, []byte("# first script\n")); err != nil {
		t.Fatal(err)
	}
	registered, err := os.ReadFile(profile)
	if err != nil || !bytes.HasPrefix(registered, original) {
		t.Fatal("existing profile lost", err)
	}
	ps7 := filepath.Join(documents, "PowerShell", "profile.ps1")
	if err := os.Remove(ps7); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "powershell.ps1")); err != nil {
		t.Fatal(err)
	}
	if err := publishPowerShellCompletion(ctx, directory, documents, []byte("# updated script\n")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(profile)
	if !bytes.Equal(after, registered) {
		t.Fatal("update duplicated or changed existing registration")
	}
	repaired, err := os.ReadFile(ps7)
	if err != nil || bytes.Count(repaired, []byte(completionBegin)) != 1 {
		t.Fatal("missing PS7 profile not repaired", err)
	}
	script, err := os.ReadFile(filepath.Join(directory, "powershell.ps1"))
	if err != nil || !bytes.Equal(script, append([]byte{0xef, 0xbb, 0xbf}, []byte("# updated script\n")...)) {
		t.Fatal("script not regenerated with UTF-8 BOM", err)
	}
}

func TestWindowsCompletionFailurePreservesProfileAndReportsCommittedInstall(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile.ps1")
	original := []byte("# SIG # Begin signature block\r\noriginal\r\n")
	if err := os.WriteFile(profile, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := appendCompletionProfile(profile); err == nil {
		t.Fatal("signed profile modified")
	}
	after, _ := os.ReadFile(profile)
	if !bytes.Equal(after, original) {
		t.Fatal("profile bytes changed")
	}
	failure := Diagnostic(&Failure{Stage: "powershell_completion", Effects: "partial", Cause: os.ErrPermission}, "update")
	if failure.Details["effects"] != "partial" || !strings.Contains(failure.Message, "Devtools was installed") {
		t.Fatal("post-install failure misreported", failure)
	}
}

func TestWindowsInstallerChildIgnoresPresentationPreference(t *testing.T) {
	t.Setenv("DEVTOOLS_FORMAT", "text")
	for _, entry := range installerEnvironment() {
		if strings.HasPrefix(strings.ToUpper(entry), "DEVTOOLS_FORMAT=") {
			t.Fatal("caller presentation leaked into installer child")
		}
	}
	if os.Getenv("DEVTOOLS_FORMAT") != "text" {
		t.Fatal("parent environment changed")
	}
}
