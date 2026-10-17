package services

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestStorageFailureMetadataNeverLeaksPathOrRawError(t *testing.T) {
	failure := storageErrorAt("read_execution_record", &os.PathError{Op: "open", Path: "SECRET-PATH-CANARY", Err: syscall.EACCES})
	body, err := json.Marshal(failure)
	if err != nil || strings.Contains(string(body), "SECRET-PATH-CANARY") || strings.Contains(string(body), "permission denied") {
		t.Fatal("raw storage error leaked", string(body), err)
	}
	if failure.Code != "io_error" || failure.ExitCode != 1 || failure.Details["os_error"] != uint64(syscall.EACCES) {
		t.Fatal("storage error contract changed", failure)
	}
}
