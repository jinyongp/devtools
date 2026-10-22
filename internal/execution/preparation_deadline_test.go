package execution

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/redact"
)

func TestPreparationBudgetSurvivesLauncherTransport(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, err := redact.Resolve(redact.Policy{}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	wire := inputWire{
		Format: 1, Args: []string{executable}, Environment: []string{},
		Directory: t.TempDir(), Executable: executable,
		Metadata: InputMetadata{Profile: "app", Selection: "none", InputID: strings.Repeat("0", 32), Output: output, PreparedAt: created},
	}
	// A parent deadline sampled before a three-second wall-clock rollback can
	// appear 32 seconds away while its monotonic budget is only 29 seconds.
	wire.Metadata.PrepareDeadline = created.Add(32 * time.Second)
	if e := validateInputWire(wire); e == nil || e.Code != "invalid_input_snapshot" {
		t.Fatal("transport accepted a window longer than the preparation limit", e)
	}
	for _, tc := range []struct {
		name         string
		budget, want time.Duration
	}{
		{"clock_rollback", 29 * time.Second, 29 * time.Second},
		{"short_parent", 5 * time.Second, 5 * time.Second},
		{"preparation_limit", DefaultPreparationTimeout, DefaultPreparationTimeout},
		{"long_parent", time.Hour, DefaultPreparationTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire.Metadata.PrepareDeadline = preparationDeadline(created, tc.budget)
			payload, e := newInputPayload(wire)
			if e != nil {
				t.Fatal(e)
			}
			defer payload.Destroy()
			encoded, e := payload.EncodeForLauncher()
			if e != nil {
				t.Fatal(e)
			}
			defer encoded.Destroy()
			transported, e := DecodeLauncherInput(encoded)
			if e != nil {
				t.Fatal("launcher rejected the preparation window", e)
			}
			defer transported.Destroy()
			metadata := transported.Metadata()
			if got := metadata.PrepareDeadline.Sub(metadata.PreparedAt); got != tc.want {
				t.Fatalf("transported budget = %v, want %v", got, tc.want)
			}
			if e := transported.Validate(created.Add(tc.want - time.Nanosecond)); e != nil {
				t.Fatal("preparation expired before its budget elapsed", e)
			}
			if e := transported.Validate(created.Add(tc.want)); e == nil || e.Code != "input_snapshot_expired" {
				t.Fatal("preparation exceeded its budget", e)
			}
		})
	}
}

func TestAuthenticatedSnapshotRetainsParentDeadline(t *testing.T) {
	command, dependencies, _ := executionSnapshotFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	plan, e := ResolvePlan(command, nil, PlanOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer plan.Destroy()
	snapshot, e := AuthenticateSnapshot(ctx, plan, dependencies)
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Destroy()
	parent, _ := ctx.Deadline()
	if snapshot.deadline.Sub(parent) != 0 {
		t.Fatal("preparation changed the parent's monotonic deadline")
	}
	if got, want := snapshot.deadline.Round(0).Sub(snapshot.created.Round(0)), parent.Sub(plan.created); got != want {
		t.Fatalf("transport budget = %v, want parent's remaining budget %v", got, want)
	}
}
