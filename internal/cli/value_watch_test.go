package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

// The observed status comes from the actual child supervisor's watcher, rather
// than a caller-triggered InspectRecordValues query.
func TestManagedSupervisorValueWatchReconcilesWithoutHintsAndKeepsAppAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app := testApp(t)
	initializeTestVault(t, app)
	root := privateTempDir(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	observed := filepath.Join(root, "observed.json")
	args, _ := json.Marshal([]string{exe, "__launcher-test-app", observed})
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='watch-fixture'\n[commands.web]\ninject=true\nexec="+string(args)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	set := func(value string) {
		t.Helper()
		if code, _, diagnostic := invoke(t, app, value, "sec", "set", "TOKEN", "--profile", "watch-fixture", "--stdin"); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	set("synthetic-watch-before")
	directory, e := app.dataDirectory()
	if e != nil {
		t.Fatal(e)
	}
	service := n05PreparedService(filepath.Dir(directory))
	started, e := service.Apply(ctx, services.Request{Action: "start", Command: "web", Directory: root, RequestID: tasks.ID()})
	if e != nil {
		t.Fatal(e)
	}
	id := started.Item.ID
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 6*time.Second)
		defer stop()
		service.Apply(cleanup, services.Request{Action: "stop", ID: id, RequestID: tasks.ID()})
	})
	wait := func(state string) services.Record {
		t.Helper()
		deadline := time.NewTimer(6 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			r, e := service.Status(ctx, id)
			if e == nil && r.ValueWatch != nil && r.ValueWatch.State == state {
				if r.State != "running" || r.EndedAt != nil {
					t.Fatal("notification stopped app")
				}
				return r
			}
			select {
			case <-deadline.C:
				t.Fatal("supervisor did not converge", state, e)
			case <-tick.C:
			}
		}
	}
	current := wait("current")
	if current.ValueWatch.Goal == nil || !current.ValueWatch.IntegrityVerified {
		t.Fatal("initial authenticated goal absent")
	}
	// Suppress notification delivery before and after the mutation. Periodic
	// reconciliation must still notice the actual new input version.
	hint := filepath.Join(service.Data, maintenance.ValueChangeHintPath)
	os.Remove(hint)
	set("synthetic-watch-after")
	if err := os.Remove(hint); err != nil {
		t.Fatal(err)
	}
	pending := wait("pending")
	if pending.ValueWatch.Goal == nil || *pending.ValueWatch.Goal == *current.ValueWatch.Goal {
		t.Fatal("changed goal missing")
	}
	after := outputData(t, app, []string{"vault", "status"})
	// Mutation legitimately touches the session; compare only after passive polling.
	time.Sleep(2200 * time.Millisecond)
	if got := outputData(t, app, []string{"vault", "status"}); mustJSON(t, after) != mustJSON(t, got) {
		t.Fatal("background polling extended session")
	}
	if code, _, diagnostic := invoke(t, app, "", "vault", "lock"); code != 0 {
		t.Fatal(diagnostic)
	}
	locked := wait("verification_pending")
	if locked.ValueWatch.Goal != nil || locked.ValueWatch.IntegrityVerified {
		t.Fatal("locked watch retained verified goal")
	}
	b, _ := json.Marshal(locked.ValueWatch)
	for _, forbidden := range []string{"synthetic-watch-before", "synthetic-watch-after", "launch_id", "session_id", "ciphertext"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatal("watch exposed value or authority")
		}
	}
	if code, _, diagnostic := invoke(t, app, "synthetic-shared-command-test-unlock\n", "vault", "unlock", "--passphrase-stdin"); code != 0 {
		t.Fatal(diagnostic)
	}
	wait("pending")
	records, e := service.List(ctx, "watch-fixture")
	if e != nil || len(records) != 1 || records[0].ID != id {
		t.Fatal("notification repeated/replaced execution", e)
	}
	content, err := os.ReadFile(observed)
	if err != nil || !strings.Contains(string(content), "synthetic-watch-before") {
		t.Fatal("old app delivery changed", err)
	}
}
