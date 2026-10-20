package diagnostics

import (
	"context"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestIssuesRetainStorageDetailsWithoutDomainPayloads(t *testing.T) {
	report := Report{Ready: true}
	report.issue("profiles", protocol.NewError("storage_error", "SECRET-MESSAGE", 1, map[string]any{
		"domain": "profiles", "operation": "create_directory", "reason": "access_denied", "os_error": uint64(5),
		"path": "SECRET-PATH", "items": []string{"SECRET-PAYLOAD"},
	}))
	if report.Ready || len(report.Issues) != 1 {
		t.Fatal(report)
	}
	details := report.Issues[0].Details
	if details["os_error"] != uint64(5) || details["path"] != nil || details["items"] != nil {
		t.Fatal(details)
	}
}

func TestExpiredDiagnosticsDeadlineIsNotUserCancellation(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := canceled(ctx); err == nil || err.Code != "deadline_exceeded" || err.ExitCode != 3 {
		t.Fatal(err)
	}
}
