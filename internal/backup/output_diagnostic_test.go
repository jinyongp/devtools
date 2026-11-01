package backup

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"strings"
	"testing"
)

func TestExistingBackupOutputExplainsCauseAndPreservation(t *testing.T) {
	e := protocol.Public(outputExistsFailure("/work/backups/app.age", "app"), "backup create")
	if e.Code != "backup_error" || e.Details["reason"] != "output_exists" || e.Details["effects"] != "none" || !strings.Contains(e.Message, "output file already exists") || e.Details["target"].(map[string]any)["name"] != "/work/backups/app.age" {
		t.Fatal(e)
	}
	if len(e.Details["remedies"].([]protocol.Remedy)) != 1 {
		t.Fatal(e.Details)
	}
}
