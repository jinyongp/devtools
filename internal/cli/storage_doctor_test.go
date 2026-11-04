package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

func TestStorageDoctorDoesNotResolveProjectOrPrepareOnInspect(t *testing.T) {
	app := testApp(t)
	t.Chdir(privateTempDir(t))
	if err := os.WriteFile("devtools.toml", []byte("invalid TOML ["), 0600); err != nil {
		t.Fatal(err)
	}
	directory, _ := app.dataDirectory()
	root := filepath.Dir(directory)
	code, out, diagnostic := invoke(t, app, "", "doctor", "--storage")
	if code != 0 || diagnostic != "" {
		t.Fatal(code, out, diagnostic)
	}
	var result struct {
		Data struct {
			Ready   bool
			Effects string
		}
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || result.Data.Ready || result.Data.Effects != "none" {
		t.Fatal(out, err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".maintenance")); !os.IsNotExist(err) {
		t.Fatal("inspection prepared storage", err)
	}
	code, out, diagnostic = invoke(t, app, "", "doctor", "--storage", "--fix")
	if code != 0 || diagnostic != "" || !strings.Contains(out, `"ready":true`) {
		t.Fatal(code, out, diagnostic)
	}
	assertContract(t, "DoctorReport", outputData(t, app, []string{"doctor", "--storage"}))
}

func TestStorageDoctorReportsLegacyAndFutureHeadersWithoutValues(t *testing.T) {
	for _, test := range []struct{ body, state string }{
		{`{"version":1,"profile":"app","keys":{"TOKEN":"private-value-sentinel"}}`, "migration_required"},
		{`{"vault_format":999,"profile":"app","values":["private-value-sentinel"]}`, "unsupported"},
	} {
		t.Run(test.state, func(t *testing.T) {
			app := testApp(t)
			directory, _ := app.dataDirectory()
			root := filepath.Dir(directory)
			release, err := maintenance.AcquireExclusive(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			release()
			if err := maintenance.Write(filepath.Join(directory, "617070.json"), []byte(test.body)); err != nil {
				t.Fatal(err)
			}
			code, out, diagnostic := invoke(t, app, "", "doctor", "--storage", "--check")
			if code != 3 || out != "" || strings.Contains(diagnostic, "private-value-sentinel") || !strings.Contains(diagnostic, `"state":"`+test.state+`"`) {
				t.Fatal(code, out, diagnostic)
			}
			assertContract(t, "DoctorReport", outputData(t, app, []string{"doctor", "--storage"}))
		})
	}
}

func TestStorageDoctorOptionConflicts(t *testing.T) {
	app := testApp(t)
	for _, args := range [][]string{{"--fix"}, {"--storage", "--fix", "--check"}, {"--storage", "--profile", "app"}, {"--storage", "--project-only"}, {"--storage", "--dir", "."}, {"--storage", "--mode", "foreground"}, {"--storage", "web"}} {
		code, out, diagnostic := invoke(t, app, "", append([]string{"doctor"}, args...)...)
		if code != 2 || out != "" || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
			t.Fatal(args, code, out, diagnostic)
		}
	}
}

func TestStorageDoctorProfileFailureRetainsVerifiedPrefixWithoutWriting(t *testing.T) {
	for _, domain := range []string{"values", "tasks"} {
		t.Run(domain, func(t *testing.T) {
			app := testApp(t)
			for _, profile := range []string{"a-checked", "b-broken", "c-unchecked"} {
				args := []string{"variable", "set", "DEMO", "--value", "private-value-sentinel", "--profile", profile}
				if domain == "tasks" {
					args = []string{"task", "add", "--title", "private-value-sentinel", "--profile", profile}
				}
				if code, out, diagnostic := invoke(t, app, "", args...); code != 0 {
					t.Fatal(code, out, diagnostic)
				}
			}
			directory, _ := app.dataDirectory()
			root := filepath.Dir(directory)
			if domain == "tasks" {
				directory = filepath.Join(root, "tasks")
			}
			path := profilekey.CanonicalPath(directory, "b-broken")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := maintenance.Write(path, []byte(`{"invalid":"private-value-sentinel"`)); err != nil {
				t.Fatal(err)
			}
			before := storageDoctorSnapshot(t, root)
			for _, operation := range []string{"--check", "--fix"} {
				for _, format := range []string{"json", "text"} {
					code, out, diagnostic := invoke(t, app, "", "--format", format, "doctor", "--storage", operation)
					if code != 3 || out != "" || strings.Contains(diagnostic, "private-value-sentinel") {
						t.Fatal(operation, format, code, out, diagnostic)
					}
					if format == "text" {
						if !strings.Contains(diagnostic, "b-broken") || !strings.Contains(diagnostic, "Storage could not be verified") || strings.Contains(diagnostic, "Storage upgrade required") || strings.Contains(diagnostic, "c-unchecked") || strings.Contains(diagnostic, "a-checked") || strings.Count(diagnostic, "Directory") != 1 {
							t.Fatal("failed profile must be identified without inventing an upgrade or a verified remainder", diagnostic)
						}
						continue
					}
					var result struct {
						Error struct {
							Details struct{ Report storageupgrade.Report }
						}
					}
					if err := json.Unmarshal([]byte(diagnostic), &result); err != nil {
						t.Fatal(err, diagnostic)
					}
					report := result.Error.Details.Report
					if report.Ready || report.Changed || report.State != "blocked" || report.Effects != "none" || len(report.Items) != 2 {
						t.Fatal("failure lost its target or changed storage", diagnostic)
					}
					if report.Items[0].Domain != domain || report.Items[0].Profile != "a-checked" || report.Items[0].State != "current" || report.Items[1].Profile != "b-broken" || report.Items[1].State != "blocked" {
						t.Fatal("verified prefix or failing profile was lost", diagnostic)
					}
				}
			}
			if !reflect.DeepEqual(before, storageDoctorSnapshot(t, root)) {
				t.Fatal("blocked check or fix changed storage")
			}
			// Only the fixture repairs its corrupt input. Diagnosis must not guess
			// replacement data or discard the unreadable profile.
			if err := maintenance.Write(path, original); err != nil {
				t.Fatal(err)
			}
			if code, out, diagnostic := invoke(t, app, "", "doctor", "--storage", "--check"); code != 0 || !strings.Contains(out, `"ready":true`) {
				t.Fatal("restoring the fixture should allow the same store to be verified", code, out, diagnostic)
			}
		})
	}
}

func TestStorageDoctorTaskLockDeadlineRetainsInspectionAndReadOnlyRecovery(t *testing.T) {
	app := testApp(t)
	for _, profile := range []string{"a-checked", "b-waiting", "c-unchecked"} {
		if code, out, diagnostic := invoke(t, app, "", "task", "add", "--profile", profile, "--title", "private-task-sentinel"); code != 0 {
			t.Fatal(code, out, diagnostic)
		}
	}
	if code, out, diagnostic := invoke(t, app, "", "doctor", "--storage", "--check"); code != 0 {
		t.Fatal("fixture must already be current", code, out, diagnostic)
	}
	directory, _ := app.dataDirectory()
	root := filepath.Dir(directory)
	before := storageDoctorSnapshot(t, root)
	lock := profilekey.CanonicalPath(filepath.Join(root, "tasks"), "b-waiting") + ".lock"
	release, err := maintenance.LockExclusive(context.Background(), lock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if release != nil {
			release()
		}
	})
	var recovery []string
	for _, operation := range []string{"--check", "--fix"} {
		for _, format := range []string{"json", "text"} {
			code, out, diagnostic := invoke(t, app, "", "--format", format, "doctor", "--storage", operation, "--timeout", "2s")
			if code != 3 || out != "" || strings.Contains(diagnostic, "private-task-sentinel") {
				t.Fatal(operation, format, code, out, diagnostic)
			}
			if format == "text" {
				if !strings.Contains(diagnostic, "timed out") || !strings.Contains(diagnostic, "b-waiting") || strings.Contains(diagnostic, "Storage upgrade required") || strings.Contains(diagnostic, "--fix") {
					t.Fatal("timeout must not claim an upgrade is required", diagnostic)
				}
				continue
			}
			var result struct {
				Error struct {
					Code    string
					Details struct {
						Report   storageupgrade.Report
						Remedies []protocol.Remedy
					}
				}
			}
			if err := json.Unmarshal([]byte(diagnostic), &result); err != nil {
				t.Fatal(err, diagnostic)
			}
			report := result.Error.Details.Report
			if result.Error.Code != "deadline_exceeded" || report.Ready || report.Changed || report.State != "blocked" || report.Effects != "none" || len(report.Items) != 2 {
				t.Fatal("deadline changed storage outcome", diagnostic)
			}
			checked, waiting := report.Items[0], report.Items[1]
			if checked.Profile != "a-checked" || checked.State != "current" || waiting.Profile != "b-waiting" || waiting.State != "blocked" || waiting.Reason != "deadline_exceeded" || waiting.Source != "task-v3" {
				t.Fatal("lost verified prefix or reported an unverified profile as current", diagnostic)
			}
			remedies := result.Error.Details.Remedies
			if len(remedies) != 1 || remedies[0].State != "ready" || remedies[0].Effect != "read" || !reflect.DeepEqual(remedies[0].Argv, []string{"devtools", "doctor", "--storage", "--check", "--timeout", "2m0s"}) {
				t.Fatal("missing executable read-only timeout recovery", diagnostic)
			}
			recovery = remedies[0].Argv
		}
	}
	release()
	release = nil
	// Windows enforces byte-range locks on reads too. Release only the fixture
	// lock before comparing every file; no writer runs between the snapshots.
	if !reflect.DeepEqual(before, storageDoctorSnapshot(t, root)) {
		t.Fatal("timed-out storage diagnosis changed fixture files")
	}
	if code, out, diagnostic := invoke(t, app, "", recovery[1:]...); code != 0 || !strings.Contains(out, `"ready":true`) {
		t.Fatal("current store should recover by releasing only the fixture lock", code, out, diagnostic)
	}
}

func TestStorageDoctorReceiptFailureKeepsProfilesAndHonorsCancellation(t *testing.T) {
	app := testApp(t)
	if code, out, diagnostic := invoke(t, app, "", "variable", "set", "DEMO", "--profile", "checked", "--value", "private-value-sentinel"); code != 0 {
		t.Fatal(code, out, diagnostic)
	}
	directory, _ := app.dataDirectory()
	root := filepath.Dir(directory)
	resource := "values-" + profilekey.Key("checked") + "-aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa.json"
	path := filepath.Join(root, "backup-receipts", resource)
	// A directory at a receipt address is unreadable as a private document on
	// every platform. It must not erase the already verified profile result.
	guard, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	before := storageDoctorSnapshot(t, root)
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		ctx = progress.WithObserver(ctx, func(event progress.Event) {
			if canceled && !event.Finished && event.Message == "Checking value operation records" {
				cancel()
			}
		})
		report, condition := app.storageEngine(root).Fix(ctx)
		cancel()
		if canceled && (condition == nil || condition.Code != "canceled") || !canceled && condition != nil {
			t.Fatal(canceled, report, condition)
		}
		if report.Ready || report.Changed || report.Effects != "none" || report.State != "blocked" || len(report.Items) != 2 {
			t.Fatal("receipt failure changed storage or discarded verified profiles", canceled, report)
		}
		var checked, blocked bool
		for _, step := range report.Items {
			checked = checked || step.Profile == "checked" && step.State == "current"
			blocked = blocked || step.Resource == resource && step.State == "blocked"
		}
		if !checked || !blocked || !reflect.DeepEqual(before, storageDoctorSnapshot(t, root)) {
			t.Fatal("receipt failure lost context or changed files", canceled, report)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if code, out, diagnostic := invoke(t, app, "", "doctor", "--storage", "--check"); code != 0 || !strings.Contains(out, `"ready":true`) {
		t.Fatal("the fixture should recover after removing only its invalid entry", code, out, diagnostic)
	}
}

func storageDoctorSnapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			files[path] = sha256.Sum256(raw)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
