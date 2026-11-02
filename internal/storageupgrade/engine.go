// Package storageupgrade coordinates explicit, recoverable domain migrations.
// It never unlocks protection or teaches ordinary domain readers old formats.
package storageupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Step struct {
	Domain      string `json:"domain"`
	Profile     string `json:"profile,omitempty"`
	Source      string `json:"source"`
	Target      string `json:"target"`
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Fingerprint string `json:"-"`
}

type Report struct {
	Scope     string `json:"scope"`
	Directory string `json:"directory"`
	State     string `json:"state"`
	Ready     bool   `json:"ready"`
	Changed   bool   `json:"changed"`
	Effects   string `json:"effects"`
	Items     []Step `json:"items"`
}

// InspectHeld runs under the existing shared maintenance gate. Apply owns its
// domain's launch/maintenance/profile gates and rechecks Step.Fingerprint there.
// Authorize runs outside all gates and may only borrow already available keys.
type Adapter struct {
	Name        string
	InspectHeld func(context.Context, string) ([]Step, error)
	Authorize   func(context.Context, []Step) (func(), error)
	Apply       func(context.Context, string, Step) (bool, error)
}

type Engine struct {
	Root     string
	Adapters []Adapter
}

type progress struct {
	Version      int               `json:"version"`
	Complete     bool              `json:"complete"`
	Steps        []Step            `json:"steps"`
	Fingerprints map[string]string `json:"fingerprints,omitempty"`
}

// Required is shared by current-format readers and writers. The CLI scopes this
// same-store remedy to the caller's selected isolation root.
func Required(domain string) *protocol.Error {
	return protocol.NewError("migration_required", "Managed storage requires an explicit upgrade before this operation.", 3, map[string]any{
		"domain": domain, "effects": "none", "retry": "after_dependency_change",
		"remedies": []protocol.Remedy{{Relation: protocol.SameStoreAction, Argv: []string{"devtools", "doctor", "--storage", "--fix"}, Effect: "value_write", RequiredInputs: []string{}, Message: "Upgrade the selected storage, then retry."}},
	})
}

func planFingerprint(items []Step) string {
	type entry struct {
		Step
		Fingerprint string `json:"fingerprint"`
	}
	entries := make([]entry, 0, len(items))
	for _, item := range items {
		if item.Domain != "coordinator" {
			entries = append(entries, entry{item, item.Fingerprint})
		}
	}
	raw, _ := json.Marshal(entries)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func failure(stage string, cause error) *protocol.Error {
	return protocol.WithStorageDetails(protocol.NewError("storage_error", "Cannot inspect or upgrade managed storage.", 1, map[string]any{"effects": "none"}), "storage", stage, "managed_storage", cause)
}

func reason(err error) string {
	var condition *protocol.Error
	if errors.As(err, &condition) {
		return condition.Code
	}
	var preparation *maintenance.QueryPreparationError
	if errors.As(err, &preparation) {
		return preparation.State
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "storage_error"
}

func (e Engine) result() Report {
	return Report{Scope: "storage", Directory: e.Root, State: "current", Ready: true, Effects: "none", Items: []Step{}}
}

func (e Engine) progressPath() string {
	return filepath.Join(e.Root, ".storage-upgrade", "progress.json")
}

func (e Engine) pendingProgress() (bool, error) {
	raw, err := maintenance.Read(e.progressPath(), 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var p progress
	if json.Unmarshal(raw, &p) != nil || p.Version != 1 {
		return false, errors.New("unsupported upgrade progress")
	}
	return !p.Complete, nil
}

func (r *Report) finalize() {
	r.Ready, r.State = true, "current"
	for _, step := range r.Items {
		if step.State != "current" {
			r.Ready = false
			if step.State == "blocked" || step.State == "unsupported" {
				r.State = "blocked"
			} else if r.State != "blocked" {
				r.State = "migration_required"
			}
		}
	}
	sort.SliceStable(r.Items, func(i, j int) bool {
		if r.Items[i].Domain == r.Items[j].Domain {
			return r.Items[i].Profile < r.Items[j].Profile
		}
		return r.Items[i].Domain < r.Items[j].Domain
	})
}

func (e Engine) Inspect(ctx context.Context) (Report, *protocol.Error) {
	r := e.result()
	if !filepath.IsAbs(e.Root) {
		return r, protocol.NewError("invalid_argument", "An absolute data root is required.", 2, nil)
	}
	if _, err := os.Lstat(e.Root); errors.Is(err, os.ErrNotExist) {
		return r, nil
	} else if err != nil {
		return r, failure("inspect_directory", err)
	}
	if err := platformfs.CheckDirectory(e.Root); err != nil {
		return r, failure("check_directory", err)
	}
	release, err := maintenance.AcquirePassiveQuery(ctx, e.Root)
	if err != nil {
		var required *maintenance.QueryPreparationError
		if errors.As(err, &required) {
			r.Items = append(r.Items, Step{Domain: "maintenance", Source: "unprepared", Target: "ready", State: "migration_required", Reason: required.State})
			r.finalize()
			return r, nil
		}
		return r, failure("acquire_lock", err)
	}
	defer release()
	for _, adapter := range e.Adapters {
		if err := ctx.Err(); err != nil {
			return r, failure("inspect_storage", err)
		}
		steps, err := adapter.InspectHeld(ctx, e.Root)
		if err != nil {
			r.Items = append(r.Items, Step{Domain: adapter.Name, Source: "unknown", Target: "current", State: "blocked", Reason: reason(err)})
			continue
		}
		r.Items = append(r.Items, steps...)
	}
	if pending, err := e.pendingProgress(); err != nil {
		return r, failure("inspect_progress", err)
	} else if pending {
		r.Items = append(r.Items, Step{Domain: "coordinator", Source: "pending", Target: "verified", State: "migration_required", Reason: "finalization_required"})
	}
	r.finalize()
	return r, nil
}

func (e Engine) writeProgress(p progress) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return maintenance.Write(e.progressPath(), raw)
}

// Fix resumes from the actual domain state, never from unverified journal claims.
// Existing domain transactions own data backup, atomic publication and recovery.
func (e Engine) Fix(ctx context.Context) (Report, *protocol.Error) {
	before, condition := e.Inspect(ctx)
	if condition != nil || before.Ready {
		return before, condition
	}
	prepared := false
	for _, step := range before.Items {
		if step.Domain == "maintenance" {
			prepared = true
		}
	}
	if prepared {
		release, err := maintenance.AcquireExclusive(ctx, e.Root)
		if err != nil {
			condition = failure("prepare_storage", err)
			condition.Details["effects"] = "unknown"
			return before, condition
		}
		release()
	}
	plan, condition := e.Inspect(ctx)
	if condition != nil {
		condition.Details["effects"] = "unknown"
		return plan, condition
	}
	if plan.Ready {
		plan.Changed = prepared
		if prepared {
			plan.Effects = "committed"
		}
		return plan, nil
	}
	blocked := map[string]string{}
	authorizedPlan := planFingerprint(plan.Items)
	for _, adapter := range e.Adapters {
		var steps []Step
		for _, step := range plan.Items {
			if step.Domain == adapter.Name && step.State == "migration_required" {
				steps = append(steps, step)
			}
		}
		if len(steps) == 0 || adapter.Authorize == nil {
			continue
		}
		release, err := adapter.Authorize(ctx, steps)
		if err != nil {
			if release != nil {
				release()
			}
			blocked[adapter.Name] = reason(err)
			continue
		}
		if release != nil {
			defer release()
		}
	}
	// This serializes coordinators, not old writers. Every adapter still takes
	// the existing writer gates; authorization is complete before this gate.
	release, err := maintenance.LockExclusive(ctx, filepath.Join(e.Root, ".storage-upgrade", "lock"))
	if err != nil {
		condition = failure("acquire_upgrade_lock", err)
		condition.Details["effects"] = "unknown"
		return plan, condition
	}
	defer release()
	plan, condition = e.Inspect(ctx)
	if condition != nil {
		condition.Details["effects"] = "unknown"
		return plan, condition
	}
	if authorizedPlan != planFingerprint(plan.Items) {
		return plan, protocol.NewError("stale_plan", "Storage changed during upgrade preparation. Inspect and retry.", 3, map[string]any{"effects": "committed", "retry": "after_input_change"})
	}
	p := progress{Version: 1, Steps: append([]Step{}, plan.Items...), Fingerprints: map[string]string{}}
	for _, step := range plan.Items {
		p.Fingerprints[step.Domain+"/"+step.Profile] = step.Fingerprint
	}
	if err := e.writeProgress(p); err != nil {
		condition = failure("stage_upgrade", err)
		condition.Details["effects"] = "unknown"
		return plan, condition
	}
	changed := prepared
	failed := map[string]string{}
	for _, step := range plan.Items {
		if step.State != "migration_required" || step.Domain == "coordinator" {
			continue
		}
		if why := blocked[step.Domain]; why != "" {
			failed[step.Domain+"/"+step.Profile] = why
			continue
		}
		for _, adapter := range e.Adapters {
			if adapter.Name != step.Domain {
				continue
			}
			if adapter.Apply == nil {
				failed[step.Domain+"/"+step.Profile] = "migration_required"
				break
			}
			didChange, err := adapter.Apply(ctx, e.Root, step)
			changed = changed || didChange
			if err != nil {
				failed[step.Domain+"/"+step.Profile] = reason(err)
			}
			if ctx.Err() != nil {
				condition = failure("apply_upgrade", ctx.Err())
				condition.Details["effects"] = "unknown"
				return plan, condition
			}
			// Save resumable progress after each domain commit. This is routing
			// metadata, never a replacement for inspecting and verifying data.
			if err := e.writeProgress(p); err != nil {
				condition = failure("record_progress", err)
				condition.Details["effects"] = "unknown"
				return plan, condition
			}
			break
		}
	}
	result, condition := e.Inspect(ctx)
	if condition != nil {
		condition.Details["effects"] = "unknown"
		return result, condition
	}
	dataReady := true
	for index := range result.Items {
		step := &result.Items[index]
		if step.Domain == "coordinator" {
			continue
		}
		if step.State != "current" {
			dataReady = false
		}
		if why := failed[step.Domain+"/"+step.Profile]; why != "" {
			step.State = "blocked"
			step.Reason = why
			dataReady = false
		}
	}
	if dataReady && len(failed) == 0 {
		p.Complete = true
		if err := e.writeProgress(p); err != nil {
			condition = failure("finalize_upgrade", err)
			condition.Details["effects"] = "unknown"
			return result, condition
		}
		result, condition = e.Inspect(ctx)
		if condition != nil {
			condition.Details["effects"] = "unknown"
			return result, condition
		}
	}
	result.Changed = changed
	result.Effects = "committed"
	if len(failed) > 0 {
		result.Effects = "unknown"
	}
	result.finalize()
	return result, nil
}
