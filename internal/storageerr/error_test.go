package storageerr

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestFailureKeepsCauseWithoutLeakingRawError(t *testing.T) {
	raw := &os.PathError{Op: "SECRET-OP", Path: "SECRET-PATH", Err: syscall.EACCES}
	wrapped := Wrap("create_directory", "data_root", "", raw)
	wrapped = Wrap("lock", "task_store", "", wrapped)
	details := Details(wrapped, "tasks", "read", "task_store")
	if details["operation"] != "create_directory" || details["resource"] != "data_root" || details["reason"] != "access_denied" || details["os_error"] != uint64(syscall.EACCES) {
		t.Fatal(details)
	}
	if !errors.Is(wrapped, os.ErrPermission) {
		t.Fatal("permission cause lost")
	}
	body, err := json.Marshal(details)
	if err != nil || strings.Contains(string(body), "SECRET") || strings.Contains(wrapped.Error(), "SECRET") {
		t.Fatal("raw error escaped", err)
	}
}

func TestLogicalFailureDoesNotInventOSCode(t *testing.T) {
	details := Details(Wrap("decode_document", "task_journal", "", ErrCorrupt), "tasks", "read", "task_store")
	if details["reason"] != "invalid_storage" || details["os_error"] != nil {
		t.Fatal(details)
	}
	if Wrap("read", "task_store", "", nil) != nil {
		t.Fatal("nil became a failure")
	}
}

func TestCleanupErrorCannotSupplyOSCodeForLogicalFailure(t *testing.T) {
	primary := Wrap("validate_document", "task_journal", "invalid_storage", ErrCorrupt)
	details := Details(errors.Join(primary, syscall.EACCES), "tasks", "read", "task_store")
	if details["reason"] != "invalid_storage" || details["os_error"] != nil {
		t.Fatal(details)
	}
}

func TestDecodeRetainsReadErrorsAndRejectsUnsafeMetadata(t *testing.T) {
	cause := Decode("decode_document", "private_document", &os.PathError{Op: "read", Path: "SECRET-PATH", Err: syscall.EACCES})
	if got := Details(cause, "tasks", "read", "task_journal"); got["reason"] != "access_denied" {
		t.Fatal(got)
	}
	safe := SafeDetails(map[string]any{
		"operation": "read_file", "reason": map[string]any{"secret": "SECRET"}, "os_error": -1,
		"resource": "C:\\SECRET", "path": "SECRET",
	})
	if len(safe) != 1 || safe["operation"] != "read_file" {
		t.Fatal(safe)
	}
}
