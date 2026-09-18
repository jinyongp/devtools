package diagnostics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestInspectEmptyStoreIsReadyAndReadOnly(t *testing.T) {
	root := t.TempDir()
	input := Input{
		Data:   filepath.Join(root, "data"),
		Config: filepath.Join(root, "config"),
		Cache:  filepath.Join(root, "cache"),
	}
	report, err := Inspect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || len(report.Issues) != 0 || len(report.Profiles.Items) != 0 || report.Processes.Total != 0 || report.Proxy.State != "stopped" || report.Dashboard.Running || report.Backup.Configured {
		t.Fatalf("unexpected empty diagnostics: %+v", report)
	}
	if _, statErr := os.Stat(input.Cache); !os.IsNotExist(statErr) {
		t.Fatalf("read-only diagnostics created cache storage: %v", statErr)
	}
}

func TestInspectDoesNotExposeStoredSecret(t *testing.T) {
	root := t.TempDir()
	input := Input{
		Data:   filepath.Join(root, "data"),
		Config: filepath.Join(root, "config"),
		Cache:  filepath.Join(root, "cache"),
	}
	store := values.Store{Directory: filepath.Join(input.Data, "profiles"), Profile: "app"}
	if _, err := store.Update(context.Background(), func(state *values.State) (bool, *protocol.Error) {
		return state.Set(values.Secret, "TOKEN", "", "CANARY-SECRET")
	}); err != nil {
		t.Fatal(err)
	}
	report, err := Inspect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	body, marshalErr := json.Marshal(report)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	text := string(body)
	for _, forbidden := range []string{"CANARY-SECRET", "\"content\"", "\"token\"", "\"context\"", "\"recipient\""} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics leaked forbidden material %q: %s", forbidden, text)
		}
	}
	if len(report.Profiles.Items) != 1 || report.Profiles.Items[0].Profile != "app" || !report.Profiles.Items[0].Values {
		t.Fatalf("profile metadata missing: %+v", report.Profiles)
	}
}

func TestInspectReportsSectionFailureWithoutDiscardingReport(t *testing.T) {
	root := t.TempDir()
	input := Input{
		Data:   filepath.Join(root, "data"),
		Config: filepath.Join(root, "config"),
		Cache:  filepath.Join(root, "cache"),
	}
	if err := os.MkdirAll(input.Config, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input.Config, "backup.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Inspect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready || report.Backup.Status != "fail" || report.Backup.ErrorCode != "storage_error" || len(report.Issues) != 1 || report.Issues[0].Section != "backup" {
		t.Fatalf("backup failure not isolated: %+v", report)
	}
}

func TestInspectUsesLiveProcessStateForProfileActiveCounts(t *testing.T) {
	root := t.TempDir()
	input := Input{
		Data:   filepath.Join(root, "data"),
		Config: filepath.Join(root, "config"),
		Cache:  filepath.Join(root, "cache"),
	}
	id := tasks.ID()
	directory := t.TempDir()
	record := services.Record{
		ID:        id,
		Profile:   "app",
		Instance:  "instance",
		Directory: directory,
		Command:   "web",
		CreatedAt: time.Now().UTC(),
		State:     "running",
	}
	recordDirectory := filepath.Join(input.Data, "processes", id)
	if err := tasks.PrivateDir(recordDirectory); err != nil {
		t.Fatal(err)
	}
	if err := tasks.WritePrivate(filepath.Join(recordDirectory, "record.json"), record); err != nil {
		t.Fatal(err)
	}

	report, err := Inspect(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if report.Processes.Active != 0 || report.Processes.Interrupted != 1 {
		t.Fatalf("live process state not detected: %+v", report.Processes)
	}
	if len(report.Profiles.Items) != 1 || report.Profiles.Items[0].ActiveProcessCount != 0 {
		t.Fatalf("profile active count disagrees with live process state: %+v", report.Profiles.Items)
	}
}
