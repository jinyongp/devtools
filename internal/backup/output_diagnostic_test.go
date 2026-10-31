package backup

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"strings"
	"testing"
)

func TestExistingBackupOutputExplainsCauseAndPreservation(t *testing.T) {
	e := protocol.Public(outputExistsFailure(), "backup create")
	if e.Code != "backup_error" || e.Details["reason"] != "output_exists" || e.Details["effects"] != "none" || !strings.Contains(e.Message, "existing file was preserved") {
		t.Fatal(e)
	}
	if len(e.Details["remedies"].([]protocol.Remedy)) != 1 {
		t.Fatal(e.Details)
	}
}
