package storageupgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/testfs"
)

func preparedRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(testfs.TempDir(t), "data")
	guard, err := platformfs.OpenRoot(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	release()
	return root
}

func TestInspectAbsentRootAndCurrentFixHaveNoWrites(t *testing.T) {
	root := filepath.Join(testfs.TempDir(t), "absent")
	report, err := (Engine{Root: root}).Inspect(context.Background())
	if err != nil || !report.Ready || report.Changed || report.Effects != "none" {
		t.Fatal(report, err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created storage", err)
	}
	root = preparedRoot(t)
	report, err = (Engine{Root: root}).Fix(context.Background())
	if err != nil || !report.Ready || report.Changed || report.Effects != "none" {
		t.Fatal(report, err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".storage-upgrade")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("current fix wrote progress", err)
	}
}

func TestFixRejectsChangesAfterAuthorization(t *testing.T) {
	root := preparedRoot(t)
	fingerprint := "before"
	applied := false
	adapter := Adapter{Name: "test", InspectHeld: func(context.Context, string) ([]Step, error) {
		return []Step{{Domain: "test", Source: "old", Target: "current", State: "migration_required", Fingerprint: fingerprint}}, nil
	}, Authorize: func(ctx context.Context, _ []Step) (func(), error) {
		// A writer must be admitted here: authorization cannot hold root locks.
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return nil, err
		}
		fingerprint = "after"
		release()
		return nil, nil
	}, Apply: func(context.Context, string, Step) (bool, error) { applied = true; return true, nil }}
	report, err := (Engine{Root: root, Adapters: []Adapter{adapter}}).Fix(context.Background())
	if err == nil || err.Code != "stale_plan" || applied || report.Ready {
		t.Fatal(report, err, applied)
	}
}

func TestPartialFixReinspectsAndResumes(t *testing.T) {
	root := preparedRoot(t)
	current := map[string]bool{}
	fail := true
	adapter := Adapter{Name: "test", InspectHeld: func(context.Context, string) ([]Step, error) {
		steps := []Step{}
		for _, profile := range []string{"one", "two"} {
			state := "migration_required"
			if current[profile] {
				state = "current"
			}
			steps = append(steps, Step{Domain: "test", Profile: profile, Source: "old", Target: "current", State: state, Fingerprint: state})
		}
		return steps, nil
	}, Apply: func(ctx context.Context, _ string, step Step) (bool, error) {
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return false, err
		}
		defer release()
		if step.Profile == "two" && fail {
			return false, errors.New("private path and secret sentinel")
		}
		current[step.Profile] = true
		return true, nil
	}}
	engine := Engine{Root: root, Adapters: []Adapter{adapter}}
	report, err := engine.Fix(context.Background())
	if err != nil || report.Ready || report.State != "blocked" || !current["one"] || current["two"] {
		t.Fatal(report, err)
	}
	fail = false
	report, err = engine.Fix(context.Background())
	if err != nil || !report.Ready || !report.Changed || !current["two"] {
		t.Fatal(report, err)
	}
	report, err = engine.Fix(context.Background())
	if err != nil || !report.Ready || report.Changed || report.Effects != "none" {
		t.Fatal(report, err)
	}
}

func TestUnknownProgressIsPreserved(t *testing.T) {
	root := preparedRoot(t)
	path := filepath.Join(root, ".storage-upgrade", "progress.json")
	if err := maintenance.Write(path, []byte(`{"version":999,"complete":false}`)); err != nil {
		t.Fatal(err)
	}
	_, err := (Engine{Root: root}).Fix(context.Background())
	if err == nil || err.Code != "storage_error" {
		t.Fatal(err)
	}
	raw, readErr := maintenance.Read(path, 1024)
	if readErr != nil || string(raw) != `{"version":999,"complete":false}` {
		t.Fatal("unknown progress overwritten", readErr)
	}
}

func TestCanceledCommitReportsEffectsAndResumes(t *testing.T) {
	root := preparedRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	current := false
	adapter := Adapter{Name: "test", InspectHeld: func(context.Context, string) ([]Step, error) {
		state := "migration_required"
		if current {
			state = "current"
		}
		return []Step{{Domain: "test", Source: "old", Target: "current", State: state, Fingerprint: state}}, nil
	}, Apply: func(context.Context, string, Step) (bool, error) { current = true; cancel(); return true, ctx.Err() }}
	engine := Engine{Root: root, Adapters: []Adapter{adapter}}
	report, condition := engine.Fix(ctx)
	if condition == nil || condition.Code != "canceled" || condition.Details["effects"] != "unknown" || report.Ready || !report.Changed || report.Effects != "unknown" {
		t.Fatal(report, condition)
	}
	report, condition = engine.Fix(context.Background())
	if condition != nil || !report.Ready || !current {
		t.Fatal("resumption lost commit", report, condition)
	}
	report, condition = engine.Fix(context.Background())
	if condition != nil || !report.Ready || report.Changed || report.Effects != "none" {
		t.Fatal("completed resumption wrote again", report, condition)
	}
}
