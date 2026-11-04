package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/retainedbody"
	"github.com/jinyongp/devtools/internal/services"
)

func TestStorageDoctorResourceFailuresPreserveCompletedTargets(t *testing.T) {
	ids := []string{
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc",
	}
	for _, domain := range []string{"processes", "process_logs", "archives", "delivery_locks"} {
		t.Run(domain, func(t *testing.T) {
			app := testApp(t)
			if code, out, diagnostic := invoke(t, app, "", "doctor", "--storage", "--fix"); code != 0 {
				t.Fatal(code, out, diagnostic)
			}
			directory, _ := app.dataDirectory()
			root := filepath.Dir(directory)
			ctx := context.Background()
			var resources []string
			var faultPath string
			lockFailure := domain == "process_logs" || domain == "delivery_locks"
			write := func(path string, body []byte) {
				t.Helper()
				if err := maintenance.Write(path, body); err != nil {
					t.Fatal(err)
				}
			}
			for index, id := range ids {
				resource, path := id, ""
				switch domain {
				case "processes":
					now := time.Now().UTC()
					if err := (services.Store{Data: root}).RestoreRecord(services.Record{ID: id, Profile: "fixture", Directory: root, Command: "web", State: "stopped", CreatedAt: now, EndedAt: &now}); err != nil {
						t.Fatal(err)
					}
					resource = filepath.Join(id, "record.json")
					path = filepath.Join(root, "processes", resource)
				case "process_logs":
					path = filepath.Join(root, "processes", id, "output.log")
					write(path, []byte("private-data-sentinel"))
					path += ".lock"
				case "archives":
					entry, _ := json.Marshal(map[string]string{"id": id})
					write(filepath.Join(root, "archives", id, "entry.json"), entry)
					store := retainedbody.Store{Root: root}
					path = filepath.Join(root, "archives", id, "payload")
					if err := store.WriteReference(ctx, path, "archive_payload", []byte("private-data-sentinel")); err != nil {
						t.Fatal(err)
					}
				case "delivery_locks":
					profile := profilekey.Key("fixture")
					executionID := strings.ReplaceAll(id, "-", "")
					resource = filepath.Join(profile, executionID+".json")
					write(filepath.Join(root, "value-executions", resource), []byte(`{"input":"private-data-sentinel"}`))
					path = filepath.Join(root, "value-execution-locks", profile, executionID+".lock")
				}
				if lockFailure {
					release, err := maintenance.LockExclusive(ctx, path)
					if err != nil {
						t.Fatal(err)
					}
					release()
				}
				resources = append(resources, resource)
				if index == 1 {
					faultPath = path
				}
			}
			if report, condition := app.storageEngine(root).Inspect(ctx); condition != nil || !report.Ready {
				t.Fatal("fixture must be current before introducing the failure", report, condition)
			}
			original, err := os.ReadFile(faultPath)
			if err != nil {
				t.Fatal(err)
			}
			if lockFailure {
				if err := os.Remove(faultPath); err != nil {
					t.Fatal(err)
				}
				guard, err := platformfs.OpenRoot(faultPath, true)
				if err != nil {
					t.Fatal(err)
				}
				if err := guard.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				write(faultPath, []byte(`{"storage_version":2,"version":2,"invalid":"private-data-sentinel"}`))
			}
			before := storageDoctorSnapshot(t, root)
			report, condition := app.storageEngine(root).Fix(ctx)
			if condition != nil || report.Ready || report.Changed || report.Effects != "none" || len(report.Items) != 2 {
				t.Fatal("blocked verification must preserve completed work without staging an upgrade", report, condition)
			}
			states := map[string]string{}
			for _, step := range report.Items {
				if step.Domain != domain {
					t.Fatal("unexpected fixture domain", step)
				}
				states[step.Resource] = step.State
			}
			if !reflect.DeepEqual(states, map[string]string{resources[0]: "current", resources[1]: "blocked"}) {
				t.Fatal("failure lost a completed target, its own address, or reported an unchecked target", report)
			}
			code, out, diagnostic := invoke(t, app, "", "--format", "text", "doctor", "--storage", "--check")
			blockedID := ids[1]
			if domain == "delivery_locks" {
				blockedID = strings.ReplaceAll(blockedID, "-", "")
			}
			if code != 3 || out != "" || !strings.Contains(diagnostic, blockedID) || !strings.Contains(diagnostic, "Storage could not be verified") || strings.Contains(diagnostic, "private-data-sentinel") {
				t.Fatal(code, out, diagnostic)
			}
			if !reflect.DeepEqual(before, storageDoctorSnapshot(t, root)) {
				t.Fatal("blocked verification changed fixture files")
			}
			if lockFailure {
				if err := os.Remove(faultPath); err != nil {
					t.Fatal(err)
				}
			}
			write(faultPath, original)
			if code, out, diagnostic := invoke(t, app, "", "doctor", "--storage", "--check"); code != 0 || !strings.Contains(out, `"ready":true`) {
				t.Fatal("repairing only the fixture input must restore verification", code, out, diagnostic)
			}
		})
	}
}
