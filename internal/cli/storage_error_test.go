package cli

import (
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestTransferRemedyPreservesOriginalStorageDetails(t *testing.T) {
	original := protocol.NewError("backup_error", "Cannot publish archive.", 3, map[string]any{
		"operation": "publish_file", "reason": "access_denied", "os_error": uint64(5), "cause": "output_exists",
	})
	got := transferArchiveError(original)
	if got.Code != original.Code || got.ExitCode != original.ExitCode || got.Details["operation"] != "publish_file" || got.Details["cause"] != "output_exists" || got.Details["remedies"] == nil {
		t.Fatal("transfer error dropped original metadata", got)
	}
	if original.Details["remedies"] != nil {
		t.Fatal("original error mutated")
	}
}
