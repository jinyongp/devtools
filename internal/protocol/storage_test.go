package protocol

import (
	"context"
	"testing"
)

func TestStorageDetailsPreserveOwnerAndExistingDetails(t *testing.T) {
	input := NewError("io_error", "Private storage unavailable.", 1, map[string]any{"remedies": []string{"inspect"}})
	got := WithStorageDetails(input, "services", "read", "process_store", nil)
	if got.Code != input.Code || got.ExitCode != input.ExitCode || got.Details["remedies"] == nil || input.Details["domain"] != nil {
		t.Fatal("owner contract changed or input mutated", got)
	}
	for _, tc := range []struct {
		cause error
		code  string
		exit  int
	}{{context.Canceled, "canceled", 130}, {context.DeadlineExceeded, "deadline_exceeded", 3}} {
		got := WithStorageDetails(input, "services", "lock", "process_store", tc.cause)
		if got.Code != tc.code || got.ExitCode != tc.exit {
			t.Fatal(got)
		}
	}
}

func TestNestedStorageFailureSurvivesDomainTranslation(t *testing.T) {
	inner := WithStorageDetails(NewError("io_error", "Process storage failed.", 1, nil), "services", "create_directory", "data_root", nil)
	inner.Details["reason"] = "access_denied"
	inner.Details["os_error"] = uint64(5)
	outer := WithStorageDetails(NewError("storage_error", "Backup failed.", 1, nil), "backup", "snapshot", "backup_store", inner)
	if outer.Code != "storage_error" || outer.Details["operation"] != "create_directory" || outer.Details["os_error"] != uint64(5) {
		t.Fatal(outer)
	}
	deadline := NewError("deadline_exceeded", "Elapsed.", 3, nil)
	if got := WithStorageDetails(outer, "backup", "snapshot", "backup_store", deadline); got != deadline {
		t.Fatal(got)
	}
}
