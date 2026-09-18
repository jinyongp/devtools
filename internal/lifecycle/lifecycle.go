package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

const (
	ActionUp      = "up"
	ActionDown    = "down"
	ActionRestart = "restart"
)

type ProcessStore interface {
	Apply(context.Context, services.Request) (services.Result, *protocol.Error)
	List(context.Context, string) ([]services.Record, *protocol.Error)
	Wait(context.Context, string, time.Duration) (services.Record, *protocol.Error)
}

type Preflight func(context.Context, project.Context, string, *string) *protocol.Error

type Manager struct {
	Data      string
	Processes ProcessStore
	Preflight Preflight
}

type Request struct {
	Action    string
	Project   project.Context
	Commands  []string
	Env       *string
	Capture   *bool
	Timeout   time.Duration
	RequestID string
}

type Condition struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type Item struct {
	Command   string           `json:"command"`
	Status    string           `json:"status"`
	Changed   bool             `json:"changed"`
	Item      *services.Record `json:"item,omitempty"`
	Condition *Condition       `json:"condition,omitempty"`
}

type Result struct {
	Action    string `json:"action"`
	Profile   string `json:"profile"`
	Directory string `json:"directory"`
	Changed   bool   `json:"changed"`
	Replayed  bool   `json:"replayed"`
	Items     []Item `json:"items"`
}

type receiptItem struct {
	Command     string `json:"command"`
	RequestID   string `json:"request_id"`
	ExecutionID string `json:"execution_id,omitempty"`
	Done        bool   `json:"done"`
	Result      Item   `json:"result"`
}

type receipt struct {
	Fingerprint string        `json:"fingerprint"`
	Action      string        `json:"action"`
	Profile     string        `json:"profile"`
	Directory   string        `json:"directory"`
	Items       []receiptItem `json:"items"`
}

type fingerprintInput struct {
	Action    string   `json:"action"`
	Profile   string   `json:"profile"`
	Directory string   `json:"directory"`
	Commands  []string `json:"commands"`
	Env       *string  `json:"env,omitempty"`
	Capture   *bool    `json:"capture_logs,omitempty"`
	Timeout   int64    `json:"timeout_ns,omitempty"`
}

var requestIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func fail(code, message string, exit int, details map[string]any) *protocol.Error {
	return protocol.NewError(code, message, exit, details)
}

func storageError() *protocol.Error {
	return fail("io_error", "Cannot access project lifecycle operation storage.", 1, nil)
}

func fingerprint(request Request) string {
	input := fingerprintInput{Action: request.Action, Profile: request.Project.Profile, Directory: request.Project.Root, Commands: append([]string{}, request.Commands...), Env: request.Env, Capture: request.Capture}
	if request.Action == ActionUp || request.Action == ActionRestart {
		input.Timeout = int64(request.Timeout)
	}
	body, _ := json.Marshal(input)
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func projectKey(profile, root string) string {
	hash := sha256.Sum256([]byte(profile + "\n" + root))
	return hex.EncodeToString(hash[:])
}

func (m Manager) base() string { return filepath.Join(m.Data, "project-operations") }
func (m Manager) receiptPath(id string) string {
	return filepath.Join(m.base(), "receipts", id+".json")
}
func (m Manager) requestLock(id string) string {
	return filepath.Join(m.base(), "request-locks", id+".lock")
}
func (m Manager) projectLock(p project.Context) string {
	return filepath.Join(m.base(), "projects", projectKey(p.Profile, p.Root), "operations.lock")
}

func validateRequest(request Request) *protocol.Error {
	if !filepath.IsAbs(request.Project.Root) || !project.ValidProfile(request.Project.Profile) || !requestIDPattern.MatchString(request.RequestID) {
		return fail("invalid_argument", "Project lifecycle request is invalid.", 2, nil)
	}
	if request.Action != ActionUp && request.Action != ActionDown && request.Action != ActionRestart {
		return fail("invalid_argument", "Project lifecycle action is invalid.", 2, nil)
	}
	if (request.Action == ActionUp || request.Action == ActionRestart) && (len(request.Commands) == 0 || request.Timeout <= 0 || request.Timeout > 10*time.Minute) {
		return fail("invalid_argument", "Project start or restart requires commands and a positive timeout up to 10m.", 2, nil)
	}
	seen := map[string]bool{}
	for _, name := range request.Commands {
		if !project.ValidProfile(name) || seen[name] {
			return fail("invalid_argument", "Project lifecycle command selection is invalid.", 2, map[string]any{"command": name})
		}
		seen[name] = true
		if request.Action == ActionUp || request.Action == ActionRestart {
			if _, ok := request.Project.Commands[name]; !ok {
				return fail("command_not_found", "The selected project command is not defined.", 3, map[string]any{"command": name})
			}
		}
	}
	return nil
}

func condition(err *protocol.Error) *Condition {
	if err == nil {
		return nil
	}
	return &Condition{Code: err.Code, Message: err.Message, Details: err.Details}
}

func pending(err *protocol.Error) bool {
	if err == nil {
		return false
	}
	switch err.Code {
	case "process_pending", "readiness_timeout", "readiness_unavailable":
		return true
	default:
		return false
	}
}

func (r receipt) result(replayed bool) Result {
	out := Result{Action: r.Action, Profile: r.Profile, Directory: r.Directory, Replayed: replayed, Items: make([]Item, 0, len(r.Items))}
	for _, child := range r.Items {
		out.Items = append(out.Items, child.Result)
		out.Changed = out.Changed || child.Result.Changed
	}
	return out
}

func (r receipt) complete() bool {
	for _, child := range r.Items {
		if !child.Done {
			return false
		}
	}
	return true
}

func (m Manager) readReceipt(id string) (receipt, error) {
	var value receipt
	err := tasks.ReadPrivate(m.receiptPath(id), &value)
	return value, err
}

func writeReceipt(path string, value receipt) *protocol.Error {
	if err := tasks.PrivateDir(filepath.Dir(path)); err != nil {
		return storageError()
	}
	if err := tasks.WritePrivate(path, value); err != nil {
		return storageError()
	}
	return nil
}

func (m Manager) initialUp(ctx context.Context, request Request, fp string) (receipt, *protocol.Error) {
	existing := map[string]bool{}
	if m.Preflight != nil {
		records, err := m.Processes.List(ctx, request.Project.Profile)
		if err != nil {
			return receipt{}, err
		}
		for _, record := range activeRecords(records, request.Project) {
			existing[record.Command] = true
		}
		for _, name := range request.Commands {
			if existing[name] {
				continue
			}
			if err := m.Preflight(ctx, request.Project, name, request.Env); err != nil {
				return receipt{}, err
			}
		}
	}
	items := make([]receiptItem, 0, len(request.Commands))
	for _, name := range request.Commands {
		items = append(items, receiptItem{Command: name, RequestID: tasks.ID(), Result: Item{Command: name, Status: "pending"}})
	}
	return receipt{Fingerprint: fp, Action: request.Action, Profile: request.Project.Profile, Directory: request.Project.Root, Items: items}, nil
}

func activeRecords(records []services.Record, p project.Context) []services.Record {
	items := []services.Record{}
	for _, record := range records {
		if record.Profile == p.Profile && record.Directory == p.Root && record.EndedAt == nil && record.State != "interrupted" {
			items = append(items, record)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Command != items[j].Command {
			return items[i].Command < items[j].Command
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func (m Manager) Status(ctx context.Context, p project.Context, commands []string) ([]services.Record, *protocol.Error) {
	if m.Processes == nil || !filepath.IsAbs(m.Data) || !filepath.IsAbs(p.Root) || !project.ValidProfile(p.Profile) {
		return nil, fail("invalid_argument", "Project lifecycle status request is invalid.", 2, nil)
	}
	selected := map[string]int{}
	for index, name := range commands {
		if !project.ValidProfile(name) || selected[name] != 0 {
			return nil, fail("invalid_argument", "Project lifecycle command selection is invalid.", 2, map[string]any{"command": name})
		}
		selected[name] = index + 1
	}
	records, err := m.Processes.List(ctx, p.Profile)
	if err != nil {
		return nil, err
	}
	items := activeRecords(records, p)
	if len(commands) == 0 {
		return items, nil
	}
	filtered := make([]services.Record, 0, len(items))
	for _, record := range items {
		if selected[record.Command] != 0 {
			filtered = append(filtered, record)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return selected[filtered[i].Command] < selected[filtered[j].Command] })
	return filtered, nil
}

func (m Manager) initialDown(ctx context.Context, request Request, fp string) (receipt, *protocol.Error) {
	records, err := m.Processes.List(ctx, request.Project.Profile)
	if err != nil {
		return receipt{}, err
	}
	active := activeRecords(records, request.Project)
	selected := request.Commands
	if len(selected) == 0 {
		seen := map[string]bool{}
		for _, record := range active {
			if !seen[record.Command] {
				selected = append(selected, record.Command)
				seen[record.Command] = true
			}
		}
		sort.Strings(selected)
	}
	items := make([]receiptItem, 0, len(selected))
	for _, name := range selected {
		child := receiptItem{Command: name, RequestID: tasks.ID(), Result: Item{Command: name, Status: "unchanged"}}
		for _, record := range active {
			if record.Command == name {
				child.ExecutionID = record.ID
				child.Result.Item = &record
				break
			}
		}
		if child.ExecutionID == "" {
			child.Done = true
		}
		items = append(items, child)
	}
	return receipt{Fingerprint: fp, Action: request.Action, Profile: request.Project.Profile, Directory: request.Project.Root, Items: items}, nil
}

func (m Manager) initialRestart(ctx context.Context, request Request, fp string) (receipt, *protocol.Error) {
	records, err := m.Processes.List(ctx, request.Project.Profile)
	if err != nil {
		return receipt{}, err
	}
	active := activeRecords(records, request.Project)
	byCommand := map[string][]services.Record{}
	for _, record := range active {
		byCommand[record.Command] = append(byCommand[record.Command], record)
	}
	items := make([]receiptItem, 0, len(request.Commands))
	for _, name := range request.Commands {
		child := receiptItem{Command: name, RequestID: tasks.ID(), Result: Item{Command: name, Status: "pending"}}
		matches := byCommand[name]
		switch len(matches) {
		case 0:
			child.Done = true
			child.Result.Status = "failed"
			child.Result.Condition = &Condition{Code: "process_not_found", Message: "No active managed execution exists for the selected project command.", Details: map[string]any{"command": name}}
		case 1:
			record := matches[0]
			child.ExecutionID = record.ID
			child.Result.Item = &record
		default:
			child.Done = true
			child.Result.Status = "failed"
			child.Result.Condition = &Condition{Code: "process_conflict", Message: "Multiple active executions exist for the selected project command.", Details: map[string]any{"command": name, "count": len(matches)}}
		}
		items = append(items, child)
	}
	return receipt{Fingerprint: fp, Action: request.Action, Profile: request.Project.Profile, Directory: request.Project.Root, Items: items}, nil
}

func (m Manager) runUp(ctx context.Context, request Request, value *receipt, path string) *protocol.Error {
	for index := range value.Items {
		child := &value.Items[index]
		if child.Done {
			continue
		}
		child.Result.Condition = nil
		processRequest := services.Request{Action: "start", Directory: request.Project.Root, Command: child.Command, Env: request.Env, Capture: request.Capture, RequestID: child.RequestID}
		if m.Preflight != nil {
			processRequest.BeforeStart = func(ctx context.Context) *protocol.Error {
				return m.Preflight(ctx, request.Project, child.Command, request.Env)
			}
		}
		result, err := m.Processes.Apply(ctx, processRequest)
		if result.Item.ID != "" {
			child.ExecutionID = result.Item.ID
			child.Result.Item = &result.Item
		}
		child.Result.Changed = child.Result.Changed || result.Changed
		if err != nil {
			child.Result.Status = "failed"
			if pending(err) {
				child.Result.Status = "pending"
			}
			child.Result.Condition = condition(err)
			if err.Code != "canceled" && !pending(err) {
				child.RequestID = tasks.ID()
				child.ExecutionID = ""
			}
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			if err.Code == "canceled" {
				return err
			}
			continue
		}
		if result.Item.EndedAt != nil || result.Item.State == "failed" || result.Item.State == "interrupted" {
			child.Result.Status = "failed"
			child.Result.Condition = &Condition{Code: "process_failed", Message: "Managed process did not remain running."}
			child.RequestID = tasks.ID()
			child.ExecutionID = ""
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			continue
		}
		if result.Item.State != "running" {
			child.Result.Status = "pending"
			child.Result.Condition = &Condition{Code: "process_pending", Message: "Managed process has not reached the running state."}
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			continue
		}
		if result.Item.ReadyConfigured {
			ready, waitErr := m.Processes.Wait(ctx, result.Item.ID, request.Timeout)
			child.Result.Item = &ready
			if waitErr != nil {
				child.Result.Status = "failed"
				if pending(waitErr) {
					child.Result.Status = "pending"
				} else if waitErr.Code != "canceled" {
					child.RequestID = tasks.ID()
					child.ExecutionID = ""
				}
				child.Result.Condition = condition(waitErr)
				if writeErr := writeReceipt(path, *value); writeErr != nil {
					return writeErr
				}
				if waitErr.Code == "canceled" {
					return waitErr
				}
				continue
			}
			child.Result.Status = "ready"
		} else if child.Result.Changed {
			child.Result.Status = "running"
		} else {
			child.Result.Status = "unchanged"
		}
		child.Done = true
		if writeErr := writeReceipt(path, *value); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func (m Manager) runDown(ctx context.Context, value *receipt, path string) *protocol.Error {
	for index := range value.Items {
		child := &value.Items[index]
		if child.Done {
			continue
		}
		child.Result.Condition = nil
		result, err := m.Processes.Apply(ctx, services.Request{Action: "stop", ID: child.ExecutionID, RequestID: child.RequestID})
		if result.Item.ID != "" {
			child.Result.Item = &result.Item
		}
		child.Result.Changed = child.Result.Changed || result.Changed
		if err != nil {
			child.Result.Status = "failed"
			if pending(err) {
				child.Result.Status = "pending"
			}
			child.Result.Condition = condition(err)
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			if err.Code == "canceled" {
				return err
			}
			continue
		}
		if child.Result.Changed {
			child.Result.Status = "stopped"
		} else {
			child.Result.Status = "unchanged"
		}
		child.Done = true
		if writeErr := writeReceipt(path, *value); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func (m Manager) runRestart(ctx context.Context, request Request, value *receipt, path string) *protocol.Error {
	for index := range value.Items {
		child := &value.Items[index]
		if child.Done {
			continue
		}
		child.Result.Condition = nil
		preflightEnv := request.Env
		if preflightEnv == nil && child.Result.Item != nil {
			preflightEnv = child.Result.Item.EnvOverride
		}
		processRequest := services.Request{
			Action:    "restart",
			ID:        child.ExecutionID,
			Env:       request.Env,
			Capture:   request.Capture,
			RequestID: child.RequestID,
		}
		if m.Preflight != nil {
			processRequest.BeforeStart = func(ctx context.Context) *protocol.Error {
				return m.Preflight(ctx, request.Project, child.Command, preflightEnv)
			}
		}
		result, err := m.Processes.Apply(ctx, processRequest)
		if result.Item.ID != "" {
			child.Result.Item = &result.Item
		}
		child.Result.Changed = child.Result.Changed || result.Changed
		if err != nil {
			child.Result.Status = "failed"
			if pending(err) {
				child.Result.Status = "pending"
			}
			child.Result.Condition = condition(err)
			if err.Code != "canceled" && !pending(err) {
				child.RequestID = tasks.ID()
			}
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			if err.Code == "canceled" {
				return err
			}
			continue
		}
		if result.Item.EndedAt != nil || result.Item.State == "failed" || result.Item.State == "interrupted" {
			child.Result.Status = "failed"
			child.Result.Condition = &Condition{Code: "process_failed", Message: "Restarted process did not remain running."}
			child.RequestID = tasks.ID()
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			continue
		}
		if result.Item.State != "running" {
			child.Result.Status = "pending"
			child.Result.Condition = &Condition{Code: "process_pending", Message: "Restarted process has not reached the running state."}
			if writeErr := writeReceipt(path, *value); writeErr != nil {
				return writeErr
			}
			continue
		}
		if result.Item.ReadyConfigured {
			ready, waitErr := m.Processes.Wait(ctx, result.Item.ID, request.Timeout)
			child.Result.Item = &ready
			if waitErr != nil {
				child.Result.Status = "failed"
				if pending(waitErr) {
					child.Result.Status = "pending"
				} else if waitErr.Code != "canceled" {
					child.RequestID = tasks.ID()
				}
				child.Result.Condition = condition(waitErr)
				if writeErr := writeReceipt(path, *value); writeErr != nil {
					return writeErr
				}
				if waitErr.Code == "canceled" {
					return waitErr
				}
				continue
			}
			child.Result.Status = "ready"
		} else {
			child.Result.Status = "running"
		}
		child.Done = true
		if writeErr := writeReceipt(path, *value); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func batchFailure(result Result) *protocol.Error {
	for _, item := range result.Items {
		if item.Condition != nil {
			exit := 3
			code := "project_operation_failed"
			message := "One or more project lifecycle operations did not complete."
			if item.Condition.Code == "canceled" {
				exit = 130
				code = "canceled"
				message = "Project lifecycle operation was canceled."
			}
			return fail(code, message, exit, map[string]any{"action": result.Action, "items": result.Items})
		}
	}
	return nil
}

func (m Manager) Apply(ctx context.Context, request Request) (Result, *protocol.Error) {
	if m.Processes == nil || !filepath.IsAbs(m.Data) {
		return Result{}, fail("internal_error", "Project lifecycle manager is not configured.", 1, nil)
	}
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	requestUnlock, err := tasks.Lock(ctx, m.requestLock(request.RequestID))
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, fail("canceled", "Project lifecycle operation was canceled.", 130, nil)
		}
		return Result{}, storageError()
	}
	defer requestUnlock()
	projectUnlock, err := tasks.Lock(ctx, m.projectLock(request.Project))
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, fail("canceled", "Project lifecycle operation was canceled.", 130, nil)
		}
		return Result{}, storageError()
	}
	defer projectUnlock()

	fp := fingerprint(request)
	path := m.receiptPath(request.RequestID)
	value, readErr := m.readReceipt(request.RequestID)
	replayed := readErr == nil
	if readErr == nil {
		if value.Fingerprint != fp {
			return Result{}, fail("request_conflict", "The request ID was already used with different project lifecycle input.", 3, nil)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Result{}, storageError()
	} else {
		var initErr *protocol.Error
		switch request.Action {
		case ActionUp:
			value, initErr = m.initialUp(ctx, request, fp)
		case ActionRestart:
			value, initErr = m.initialRestart(ctx, request, fp)
		default:
			value, initErr = m.initialDown(ctx, request, fp)
		}
		if initErr != nil {
			return Result{}, initErr
		}
		if writeErr := writeReceipt(path, value); writeErr != nil {
			return Result{}, writeErr
		}
	}
	if value.complete() {
		result := value.result(replayed)
		if failure := batchFailure(result); failure != nil {
			return result, failure
		}
		return result, nil
	}
	var runErr *protocol.Error
	switch request.Action {
	case ActionUp:
		runErr = m.runUp(ctx, request, &value, path)
	case ActionRestart:
		runErr = m.runRestart(ctx, request, &value, path)
	default:
		runErr = m.runDown(ctx, &value, path)
	}
	result := value.result(replayed)
	if runErr != nil {
		return result, runErr
	}
	if failure := batchFailure(result); failure != nil {
		return result, failure
	}
	return result, nil
}
