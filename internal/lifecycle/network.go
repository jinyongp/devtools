package lifecycle

import (
	"context"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func (r receipt) hasProxyExecutions() bool {
	for _, child := range r.Items {
		if child.Result.Item != nil && child.Result.Item.ProxyMode {
			return true
		}
	}
	return len(r.NetworkSnapshots) > 0
}

// runNetwork launches the whole selected batch before waiting. The original
// execution ID remains the restart target throughout partial retries.
func (m Manager) runNetwork(ctx context.Context, request Request, value *receipt, path string) *protocol.Error {
	for index := range value.Items {
		child := &value.Items[index]
		if child.Done || child.Launched {
			continue
		}
		child.Result.Condition = nil
		q := services.Request{Action: "start", Directory: request.Project.Root, Command: child.Command, RequestID: child.RequestID, Env: request.Env, Bundle: request.Bundle, Capture: request.Capture}
		if request.Action == ActionRestart {
			q.Action = "restart"
			q.Directory = ""
			q.Command = ""
			q.ID = child.ExecutionID
		}
		snapshot := value.NetworkSnapshots[child.Command]
		q.ProxyMode = snapshot != nil
		q.Network = snapshot
		if request.Action == ActionRestart {
			q.RestartPreflight = func(ctx context.Context, record services.Record, phase services.RestartPhase) *protocol.Error {
				env := request.Env
				if env == nil {
					env = record.EnvOverride
				}
				bundle := request.Bundle
				if bundle == nil {
					bundle = record.BundleOverride
				}
				beforeStop := phase == services.RestartBeforeStop
				if snapshot != nil && m.SnapshotPreflight != nil {
					return m.SnapshotPreflight(ctx, request.Project, child.Command, env, snapshot, beforeStop, bundle)
				}
				if beforeStop && m.Validate != nil {
					return m.Validate(ctx, request.Project, child.Command, env, bundle)
				}
				if !beforeStop && m.Preflight != nil {
					return m.Preflight(ctx, request.Project, child.Command, env, bundle)
				}
				return nil
			}
		} else if snapshot != nil && m.SnapshotPreflight != nil {
			q.BeforeStart = func(ctx context.Context) *protocol.Error {
				return m.SnapshotPreflight(ctx, request.Project, child.Command, request.Env, snapshot, false, request.Bundle)
			}
		} else if m.Preflight != nil {
			q.BeforeStart = func(ctx context.Context) *protocol.Error {
				return m.Preflight(ctx, request.Project, child.Command, request.Env, request.Bundle)
			}
		}
		result, e := m.Processes.Apply(ctx, q)
		if result.Item.ID != "" {
			child.Result.Item = &result.Item
			if request.Action == ActionUp {
				child.ExecutionID = result.Item.ID
			}
		}
		child.Result.Changed = child.Result.Changed || result.Changed
		if e != nil {
			child.Result.Status = "failed"
			if pending(e) {
				child.Result.Status = "pending"
			}
			child.Result.Condition = condition(e)
			if e.Code != "canceled" && !pending(e) {
				if terminalChild(result.Item, e) {
					child.Done = true
				} else {
					child.RequestID = tasks.ID()
					if request.Action == ActionUp {
						child.ExecutionID = ""
					}
				}
			}
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			if e.Code == "canceled" {
				return e
			}
			continue
		}
		if result.Item.State != "running" || result.Item.EndedAt != nil {
			child.Result.Status = "pending"
			child.Result.Condition = &Condition{Code: "process_pending", Message: "Managed process has not reached the running state."}
			if result.Item.EndedAt != nil || result.Item.State == "failed" || result.Item.State == "interrupted" {
				child.Result.Status = "failed"
				child.Result.Condition.Code = "process_failed"
				if terminalChild(result.Item, nil) {
					child.Done = true
				} else {
					child.RequestID = tasks.ID()
				}
			}
			if e := writeReceipt(path, *value); e != nil {
				return e
			}
			continue
		}
		child.Launched = true
		child.Result.Status = "running"
		if e := writeReceipt(path, *value); e != nil {
			return e
		}
	}
	for index := range value.Items {
		child := &value.Items[index]
		if child.Done || !child.Launched || child.Result.Item == nil {
			continue
		}
		record := *child.Result.Item
		if record.ReadyConfigured {
			ready, e := m.Processes.Wait(ctx, record.ID, request.Timeout)
			if ready.ID != "" {
				child.Result.Item = &ready
			}
			if e != nil {
				child.Result.Status = "failed"
				if pending(e) {
					child.Result.Status = "pending"
				}
				child.Result.Condition = condition(e)
				if !pending(e) && e.Code != "canceled" {
					if terminalChild(record, e) {
						child.Done = true
					} else {
						child.Launched = false
						child.RequestID = tasks.ID()
					}
				}
				if writeErr := writeReceipt(path, *value); writeErr != nil {
					return writeErr
				}
				if e.Code == "canceled" {
					return e
				}
				continue
			}
			child.Result.Status = "ready"
		} else if !child.Result.Changed {
			child.Result.Status = "unchanged"
		} else {
			child.Result.Status = "running"
		}
		child.Result.Condition = nil
		child.Done = true
		if e := writeReceipt(path, *value); e != nil {
			return e
		}
	}
	return nil
}
