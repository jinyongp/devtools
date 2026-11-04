// Package storageupgrade coordinates explicit, recoverable domain migrations.
// It never unlocks protection or teaches ordinary domain readers old formats.
package storageupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	operationprogress "github.com/jinyongp/devtools/internal/progress"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Step struct {
	Domain      string         `json:"domain"`
	Profile     string         `json:"profile,omitempty"`
	Resource    string         `json:"resource,omitempty"`
	Source      string         `json:"source"`
	Target      string         `json:"target"`
	State       string         `json:"state"`
	Reason      string         `json:"reason,omitempty"`
	Diagnostic  map[string]any `json:"diagnostic,omitempty"`
	Fingerprint string         `json:"-"`
}

// InspectionFailed retains the known target and format without claiming its
// payload was verified. Diagnostics follow the same redaction policy as queries.
func (s Step) InspectionFailed(cause error) Step {
	if s.Source == "" {
		s.Source = "unknown"
	}
	if s.Target == "" {
		s.Target = "current"
	}
	s.State, s.Reason = "blocked", reason(cause)
	s.Diagnostic = diagnostic(s.Domain, "inspect_storage", cause)
	return s
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

// InspectHeld runs under the existing shared maintenance gate. On error it may
// return a verified prefix followed by the blocked target; uninspected targets
// must not be returned as current. Apply owns its
// domain's launch/maintenance/profile gates and rechecks Step.Fingerprint there.
// Authorize runs outside all gates and may only borrow already available keys.
type Adapter struct {
	Name        string
	InspectHeld func(context.Context, string) ([]Step, error)
	Authorize   func(context.Context, []Step) (func(), error)
	Apply       func(context.Context, string, Step) (bool, error)
	Recover     func(context.Context, string) (bool, error)
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

func stepKey(step Step) string { return step.Domain + "/" + step.Profile + "/" + step.Resource }

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

func failure(stage string, cause error) (result *protocol.Error) {
	defer func() {
		if result.Details == nil {
			result.Details = map[string]any{}
		}
		if result.Details["effects"] == nil {
			result.Details["effects"] = "none"
		}
	}()
	var condition *protocol.Error
	if errors.As(cause, &condition) {
		return protocol.WithStorageDetails(condition, "storage", stage, "managed_storage", cause)
	}
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

func diagnostic(domain, stage string, cause error) map[string]any {
	var condition *protocol.Error
	if !errors.As(cause, &condition) {
		condition = protocol.WithStorageDetails(protocol.NewError("storage_error", "Managed storage is unavailable.", 1, nil), domain, stage, "managed_storage", cause)
	}
	return protocol.ObservationDiagnostic(condition)
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
			if r.Items[i].Profile == r.Items[j].Profile {
				return r.Items[i].Resource < r.Items[j].Resource
			}
			return r.Items[i].Profile < r.Items[j].Profile
		}
		return r.Items[i].Domain < r.Items[j].Domain
	})
}

func (e Engine) Inspect(ctx context.Context) (r Report, condition *protocol.Error) {
	finish := operationprogress.Begin(ctx, "Inspecting managed storage")
	defer finish()
	r = e.result()
	defer func() {
		if condition != nil {
			r.Ready = false
			r.State = "blocked"
		}
	}()
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
		finishDomain := operationprogress.Begin(ctx, "Checking "+adapter.Name+" storage")
		steps, err := adapter.InspectHeld(ctx, e.Root)
		finishDomain()
		r.Items = append(r.Items, steps...)
		if err != nil {
			if len(steps) == 0 || steps[len(steps)-1].State != "blocked" {
				r.Items = append(r.Items, (Step{Domain: adapter.Name}).InspectionFailed(err))
			}
			continue
		}
	}
	if err := ctx.Err(); err != nil {
		return r, failure("inspect_storage", err)
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
func (e Engine) Fix(ctx context.Context) (report Report, condition *protocol.Error) {
	finish := operationprogress.Begin(ctx, "Upgrading managed storage")
	defer finish()
	wrote := false
	defer func() {
		if wrote {
			report.Changed = true
			if report.Effects == "none" {
				report.Effects = "committed"
			}
		}
		if condition != nil {
			report.Ready = false
			report.State = "blocked"
			if wrote {
				report.Effects = "unknown"
				if condition.Details == nil {
					condition.Details = map[string]any{}
				}
				condition.Details["effects"] = "unknown"
			} else if effect, _ := condition.Details["effects"].(string); effect != "" && effect != "none" {
				report.Effects = "unknown"
			}
		}
	}()
	before, condition := e.Inspect(ctx)
	if condition != nil || before.Ready {
		return before, condition
	}
	needsUpgrade := false
	for _, step := range before.Items {
		needsUpgrade = needsUpgrade || step.State == "migration_required"
	}
	if !needsUpgrade {
		return before, nil
	}
	prepared := false
	recovery := false
	for _, step := range before.Items {
		if step.Domain == "maintenance" {
			prepared = true
			recovery = step.Reason == "recovery_required"
		}
	}
	if prepared {
		release, err := maintenance.AcquireExclusive(ctx, e.Root)
		if err != nil && recovery {
			for _, adapter := range e.Adapters {
				if adapter.Recover == nil {
					continue
				}
				var destroy func()
				if adapter.Authorize != nil {
					destroy, err = adapter.Authorize(ctx, nil)
					if err != nil {
						if destroy != nil {
							destroy()
						}
						break
					}
				}
				_, err = adapter.Recover(ctx, e.Root)
				if destroy != nil {
					destroy()
				}
				if err != nil {
					break
				}
				release, err = maintenance.AcquireExclusive(ctx, e.Root)
				break
			}
		}
		if err != nil {
			condition = failure("prepare_storage", err)
			condition.Details["effects"] = "unknown"
			return before, condition
		}
		release()
		wrote = true
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
	blocked := map[string]error{}
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
		finishAccess := operationprogress.Begin(ctx, "Checking access to "+adapter.Name+" storage")
		release, err := adapter.Authorize(ctx, steps)
		finishAccess()
		if err != nil {
			if release != nil {
				release()
			}
			blocked[adapter.Name] = err
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
		p.Fingerprints[stepKey(step)] = step.Fingerprint
	}
	if err := e.writeProgress(p); err != nil {
		condition = failure("stage_upgrade", err)
		condition.Details["effects"] = "unknown"
		return plan, condition
	}
	wrote = true
	changed := prepared
	failed := map[string]error{}
	upgradeIndex, upgradeTotal := 0, 0
	for _, step := range plan.Items {
		if step.State == "migration_required" && step.Domain != "coordinator" {
			upgradeTotal++
		}
	}
	for _, step := range plan.Items {
		if step.State != "migration_required" || step.Domain == "coordinator" {
			continue
		}
		upgradeIndex++
		if why := blocked[step.Domain]; why != nil {
			failed[stepKey(step)] = why
			continue
		}
		for _, adapter := range e.Adapters {
			if adapter.Name != step.Domain {
				continue
			}
			if adapter.Apply == nil {
				failed[stepKey(step)] = Required(step.Domain)
				break
			}
			finishStep := operationprogress.Begin(ctx, fmt.Sprintf("Upgrading %s storage (%d/%d)", step.Domain, upgradeIndex, upgradeTotal))
			didChange, err := adapter.Apply(ctx, e.Root, step)
			finishStep()
			changed = changed || didChange
			if err != nil {
				failed[stepKey(step)] = err
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
		if why := failed[stepKey(*step)]; why != nil {
			step.State = "blocked"
			step.Reason = reason(why)
			step.Diagnostic = diagnostic(step.Domain, "upgrade_storage", why)
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
