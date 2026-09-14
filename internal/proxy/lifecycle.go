package proxy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

const DefaultPort = 20200
const noProxyAttempt = "none"

type Record struct {
	CreatedAt time.Time  `json:"created_at"`
	AttemptID string     `json:"attempt_id,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	State     string     `json:"state"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Reason    string     `json:"reason"`
}

type Status struct {
	Running   bool       `json:"running"`
	State     string     `json:"state"`
	Port      int        `json:"port"`
	URL       string     `json:"url"`
	StartedAt *time.Time `json:"started_at"`
	Reason    string     `json:"reason"`
	Changed   bool       `json:"changed,omitempty"`
	Replayed  bool       `json:"replayed,omitempty"`
}

type Request struct {
	Action    string `json:"action"`
	Port      *int   `json:"port,omitempty"`
	RequestID string `json:"request_id"`
}

type receipt struct {
	Fingerprint     string        `json:"fingerprint"`
	AttemptID       string        `json:"attempt_id,omitempty"`
	PreviousAttempt string        `json:"previous_attempt,omitempty"`
	Result          Status        `json:"result"`
	Error           *receiptError `json:"error,omitempty"`
	ErrorCode       string        `json:"error_code,omitempty"`
	Done            bool          `json:"done"`
}

type receiptError struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
	ExitCode int            `json:"exit_code"`
}

type proxyControl struct {
	Address string `json:"address"`
	Token   string `json:"token"`
}

type connectionRegistry struct {
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	closing     bool
}

type trackedListener struct {
	net.Listener
	registry *connectionRegistry
}

type trackedConnection struct {
	net.Conn
	registry *connectionRegistry
	once     sync.Once
}

func (listener trackedListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return listener.registry.track(connection), nil
}

func (registry *connectionRegistry) track(connection net.Conn) net.Conn {
	tracked := &trackedConnection{Conn: connection, registry: registry}
	registry.mu.Lock()
	closing := registry.closing
	if !closing {
		if registry.connections == nil {
			registry.connections = map[net.Conn]struct{}{}
		}
		registry.connections[tracked] = struct{}{}
	}
	registry.mu.Unlock()
	if closing {
		_ = tracked.Close()
	}
	return tracked
}

func (connection *trackedConnection) Close() error {
	err := connection.Conn.Close()
	connection.once.Do(func() {
		connection.registry.mu.Lock()
		delete(connection.registry.connections, connection)
		connection.registry.mu.Unlock()
	})
	return err
}

func (registry *connectionRegistry) closeAll() {
	registry.mu.Lock()
	registry.closing = true
	connections := make([]net.Conn, 0, len(registry.connections))
	for connection := range registry.connections {
		connections = append(connections, connection)
	}
	registry.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

type Manager struct {
	Data          string
	Spawn         func(string, string) error
	Listen        func(int) ([]net.Listener, error)
	ControlListen func() (net.Listener, error)
	commit        func(string, any) error
	readyTimeout  time.Duration
	stopTimeout   time.Duration
}

func proxyFailure(code string) *protocol.Error {
	exit := 3
	if code == "invalid_argument" {
		exit = 2
	}
	if code == "canceled" {
		exit = 130
	}
	return protocol.NewError(code, "Proxy operation could not satisfy the requested condition.", exit, nil)
}

func proxyStorageError() *protocol.Error {
	return protocol.NewError("io_error", "Cannot access private proxy storage.", 1, nil)
}

func (m Manager) root() string            { return filepath.Join(m.Data, "proxy") }
func (m Manager) path(name string) string { return filepath.Join(m.root(), name) }
func (m Manager) portStore() ports.Store {
	return ports.Store{Directory: filepath.Join(m.Data, "ports")}
}

func (m Manager) readRecord() (Record, error) {
	var record Record
	return record, tasks.ReadPrivate(m.path("record.json"), &record)
}

func (m Manager) write(name string, value any) error {
	path := m.path(name)
	if err := tasks.PrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	if m.commit != nil {
		return m.commit(path, value)
	}
	return tasks.WritePrivate(path, value)
}

func (m Manager) reservedPort() (int, *protocol.Error) {
	state, err := m.portStore().Read()
	if err != nil {
		return 0, err
	}
	reservation := state.Reservation("proxy")
	if reservation == nil {
		return 0, nil
	}
	return reservation.Port, nil
}

func (m Manager) Status(ctx context.Context) (Status, *protocol.Error) {
	port, err := m.reservedPort()
	if err != nil {
		return Status{}, err
	}
	status := Status{State: "stopped", Port: port}
	if port != 0 {
		status.URL = "http://localhost:" + strconv.Itoa(port)
	}
	record, readErr := m.readRecord()
	if errors.Is(readErr, os.ErrNotExist) {
		return status, nil
	}
	if readErr != nil {
		return Status{}, proxyStorageError()
	}
	status.State, status.StartedAt, status.Reason = record.State, record.StartedAt, record.Reason
	if record.EndedAt != nil {
		return status, nil
	}
	if live, rpcErr := m.rpc(ctx, "status"); rpcErr == nil {
		status.State, status.StartedAt, status.Reason = live.State, live.StartedAt, live.Reason
		status.Running = live.State == "running"
		return status, nil
	}
	if record.State == "starting" && !record.CreatedAt.IsZero() && time.Since(record.CreatedAt) < 2*time.Second {
		return status, nil
	}
	if m.leaseFree(ctx) {
		status.State = "interrupted"
		status.Reason = "supervisor_lost"
	} else {
		status.State = "unknown"
		status.Reason = "supervisor_unavailable"
	}
	return status, nil
}

func (m Manager) Apply(ctx context.Context, request Request) (Status, *protocol.Error) {
	if !validRequestID(request.RequestID) || request.Action != "start" && request.Action != "stop" || request.Action == "stop" && request.Port != nil {
		return Status{}, proxyFailure("invalid_argument")
	}
	release, lockErr := tasks.Lock(ctx, m.path("operations.lock"))
	if lockErr != nil {
		if ctx.Err() != nil {
			return Status{}, proxyFailure("canceled")
		}
		return Status{}, proxyStorageError()
	}
	defer release()
	encoded, _ := json.Marshal(request)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	receiptPath := filepath.Join("receipts", request.RequestID+".json")
	var old receipt
	receiptExists := false
	if readErr := tasks.ReadPrivate(m.path(receiptPath), &old); readErr == nil {
		receiptExists = true
		if old.Fingerprint != fingerprint {
			return Status{}, proxyFailure("request_conflict")
		}
		if old.Done {
			if old.Error != nil {
				return Status{}, &protocol.Error{Code: old.Error.Code, Message: old.Error.Message, Details: old.Error.Details, ExitCode: old.Error.ExitCode}
			}
			if old.ErrorCode != "" {
				return Status{}, proxyFailure(old.ErrorCode)
			}
			old.Result.Replayed = true
			return old.Result, nil
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Status{}, proxyStorageError()
	}
	if !receiptExists {
		if err := m.write(receiptPath, receipt{Fingerprint: fingerprint}); err != nil {
			return Status{}, proxyStorageError()
		}
	}
	var result Status
	var err *protocol.Error
	anchor := func(attemptID, previousAttempt string) *protocol.Error {
		if old.AttemptID == attemptID && (previousAttempt == "" || old.PreviousAttempt == previousAttempt) {
			return nil
		}
		if writeErr := m.write(receiptPath, receipt{Fingerprint: fingerprint, AttemptID: attemptID, PreviousAttempt: previousAttempt}); writeErr != nil {
			return proxyStorageError()
		}
		old.AttemptID = attemptID
		return nil
	}
	if request.Action == "start" {
		if old.AttemptID != "" {
			result, err = m.resumeStart(ctx, request.Port, request.RequestID, old.AttemptID, old.PreviousAttempt, anchor)
		} else {
			result, err = m.start(ctx, request.Port, request.RequestID, anchor)
		}
	} else {
		result, err = m.stopAttempt(ctx, old.AttemptID, anchor)
	}
	if err != nil {
		if err.Code != "proxy_pending" {
			stored := &receiptError{Code: err.Code, Message: err.Message, Details: err.Details, ExitCode: err.ExitCode}
			if writeErr := m.write(receiptPath, receipt{Fingerprint: fingerprint, Error: stored, Done: true}); writeErr != nil {
				return Status{}, proxyStorageError()
			}
		}
		return Status{}, err
	}
	if writeErr := m.write(receiptPath, receipt{Fingerprint: fingerprint, Result: result, Done: true}); writeErr != nil {
		return Status{}, proxyStorageError()
	}
	return result, nil
}

func (m Manager) resumeStart(ctx context.Context, requested *int, requestID, attemptID, previousAttempt string, anchor func(string, string) *protocol.Error) (Status, *protocol.Error) {
	record, err := m.readRecord()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Status{}, proxyStorageError()
	}
	if err == nil && record.AttemptID == attemptID {
		if record.State == "failed" && record.Reason == "supervisor_start_timeout" && record.EndedAt != nil {
			if anchorErr := anchor(attemptID, attemptID); anchorErr != nil {
				return Status{}, anchorErr
			}
			recovered, recoveredRecord, recoverErr := m.recoverTimedOutAttempt(ctx, attemptID)
			if recoverErr != nil {
				return Status{}, recoverErr
			}
			if recovered {
				return m.spawnAttempt(ctx, recoveredRecord)
			}
		}
		if record.EndedAt != nil || record.State != "starting" && record.State != "running" {
			return Status{}, proxyFailure("proxy_start_failed")
		}
		port, portErr := m.reservedPort()
		if portErr != nil {
			return Status{}, portErr
		}
		if requested != nil && *requested != port {
			return Status{}, proxyFailure("proxy_conflict")
		}
		status, waitErr := m.awaitStart(ctx, attemptID, record.CreatedAt)
		if waitErr != nil {
			return Status{}, waitErr
		}
		if !status.Running {
			if status.State == "failed" && status.Reason == "supervisor_start_timeout" {
				if anchorErr := anchor(attemptID, attemptID); anchorErr != nil {
					return Status{}, anchorErr
				}
				recovered, recoveredRecord, recoverErr := m.recoverTimedOutAttempt(ctx, attemptID)
				if recoverErr != nil {
					return Status{}, recoverErr
				}
				if recovered {
					return m.spawnAttempt(ctx, recoveredRecord)
				}
			}
			return Status{}, proxyFailure("proxy_start_failed")
		}
		status.Changed = previousAttempt != ""
		return status, nil
	}
	if !matchesPreviousAttempt(record, err, previousAttempt) {
		return Status{}, proxyFailure("proxy_start_failed")
	}
	return m.installAnchoredAttempt(ctx, requested, requestID, attemptID, previousAttempt)
}

func matchesPreviousAttempt(record Record, readErr error, previousAttempt string) bool {
	if previousAttempt == noProxyAttempt {
		return errors.Is(readErr, os.ErrNotExist)
	}
	return readErr == nil && record.AttemptID == previousAttempt && record.EndedAt != nil
}

func (m Manager) installAnchoredAttempt(ctx context.Context, requested *int, requestID, attemptID, previousAttempt string) (Status, *protocol.Error) {
	release, lockErr := tasks.Lock(ctx, m.path("transition.lock"))
	if lockErr != nil {
		if ctx.Err() != nil {
			return Status{}, proxyFailure("canceled")
		}
		return Status{}, proxyStorageError()
	}
	latest, readErr := m.readRecord()
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		release()
		return Status{}, proxyStorageError()
	}
	if !matchesPreviousAttempt(latest, readErr, previousAttempt) {
		release()
		return Status{}, proxyFailure("proxy_start_failed")
	}
	port, portErr := m.reservedPort()
	if portErr != nil {
		release()
		return Status{}, portErr
	}
	if requested != nil {
		port = *requested
	}
	if port == 0 {
		port = DefaultPort
	}
	if port < 1 || port > 65535 {
		release()
		return Status{}, proxyFailure("invalid_argument")
	}
	port, _, reserveErr := m.portStore().Reserve(ctx, "proxy", port)
	if reserveErr != nil {
		release()
		return Status{}, reserveErr
	}
	record := Record{CreatedAt: time.Now().UTC(), AttemptID: attemptID, RequestID: requestID, State: "starting"}
	if writeErr := m.write("record.json", record); writeErr != nil {
		release()
		return Status{}, proxyStorageError()
	}
	release()
	return m.spawnAttempt(ctx, record)
}

func (m Manager) spawnAttempt(ctx context.Context, record Record) (Status, *protocol.Error) {
	spawn := m.Spawn
	if spawn == nil {
		spawn = spawnProxy
	}
	if spawnErr := spawn(m.Data, record.AttemptID); spawnErr != nil {
		now := time.Now().UTC()
		_, changed, transitionErr := m.changeAttempt(context.Background(), record.AttemptID, func(active *Record) bool {
			if active.State != "starting" || active.EndedAt != nil {
				return false
			}
			active.State, active.Reason, active.EndedAt = "failed", "supervisor_start_failed", &now
			return true
		})
		if transitionErr != nil {
			return Status{}, proxyStorageError()
		}
		if changed {
			return Status{}, proxyFailure("proxy_start_failed")
		}
		status, waitErr := m.awaitStart(ctx, record.AttemptID, record.CreatedAt)
		if waitErr != nil {
			return Status{}, waitErr
		}
		if status.Running {
			status.Changed = true
			return status, nil
		}
		return Status{}, proxyFailure("proxy_start_failed")
	}
	status, waitErr := m.awaitStart(ctx, record.AttemptID, record.CreatedAt)
	if waitErr != nil {
		return Status{}, waitErr
	}
	if !status.Running {
		return Status{}, proxyFailure("proxy_start_failed")
	}
	status.Changed = true
	return status, nil
}

func (m Manager) start(ctx context.Context, requested *int, requestID string, anchor func(string, string) *protocol.Error) (Status, *protocol.Error) {
	for {
		if ctx.Err() != nil {
			return Status{}, proxyFailure("canceled")
		}
		current, err := m.Status(ctx)
		if err != nil {
			return Status{}, err
		}
		record, readErr := m.readRecord()
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return Status{}, proxyStorageError()
		}
		if current.Running {
			if requested != nil && *requested != current.Port {
				return Status{}, proxyFailure("proxy_conflict")
			}
			if readErr != nil || record.State != "running" || record.EndedAt != nil {
				continue
			}
			if anchorErr := anchor(record.AttemptID, ""); anchorErr != nil {
				return Status{}, anchorErr
			}
			return current, nil
		}
		inFlight := readErr == nil && record.State == "starting" && record.EndedAt == nil
		if inFlight {
			if requested != nil && *requested != current.Port {
				return Status{}, proxyFailure("proxy_conflict")
			}
			if anchorErr := anchor(record.AttemptID, ""); anchorErr != nil {
				return Status{}, anchorErr
			}
			current, err = m.awaitStart(ctx, record.AttemptID, record.CreatedAt)
			if err != nil {
				return Status{}, err
			}
			if current.Running {
				return current, nil
			}
			if current.State == "failed" {
				if current.Reason == "supervisor_start_timeout" {
					if anchorErr := anchor(record.AttemptID, record.AttemptID); anchorErr != nil {
						return Status{}, anchorErr
					}
					recovered, recoveredRecord, recoverErr := m.recoverTimedOutAttempt(ctx, record.AttemptID)
					if recoverErr != nil {
						return Status{}, recoverErr
					}
					if recovered {
						return m.spawnAttempt(ctx, recoveredRecord)
					}
				}
				return Status{}, proxyFailure("proxy_start_failed")
			}
		}
		if readErr == nil && record.RequestID == requestID && record.State == "running" && record.EndedAt == nil && current.State == "unknown" {
			if anchorErr := anchor(record.AttemptID, ""); anchorErr != nil {
				return Status{}, anchorErr
			}
			current, err = m.awaitStart(ctx, record.AttemptID, record.CreatedAt)
			if err != nil {
				return Status{}, err
			}
			if current.Running {
				return current, nil
			}
			if current.State == "failed" {
				return Status{}, proxyFailure("proxy_start_failed")
			}
		}
		if readErr == nil && record.State == "running" && record.EndedAt == nil && !current.Running && current.StartedAt == nil {
			if current.State == "starting" || current.State == "interrupted" && !m.leaseFree(ctx) {
				continue
			}
		}
		if current.State == "unknown" {
			if requested != nil && *requested != current.Port {
				return Status{}, proxyFailure("proxy_conflict")
			}
			return Status{}, proxyFailure("proxy_unavailable")
		}
		port := current.Port
		if requested != nil {
			port = *requested
		}
		if port == 0 {
			port = DefaultPort
		}
		if port < 1 || port > 65535 {
			return Status{}, proxyFailure("invalid_argument")
		}
		attemptBytes := make([]byte, 16)
		if _, randomErr := rand.Read(attemptBytes); randomErr != nil {
			return Status{}, proxyStorageError()
		}
		next := Record{AttemptID: hex.EncodeToString(attemptBytes), RequestID: requestID, State: "starting"}
		release, transitionErr := tasks.Lock(ctx, m.path("transition.lock"))
		if transitionErr != nil {
			if ctx.Err() != nil {
				return Status{}, proxyFailure("canceled")
			}
			return Status{}, proxyStorageError()
		}
		latest, latestErr := m.readRecord()
		if latestErr != nil && !errors.Is(latestErr, os.ErrNotExist) {
			release()
			return Status{}, proxyStorageError()
		}
		if !sameRecordSnapshot(record, readErr, latest, latestErr) {
			release()
			continue
		}
		previousAttempt := noProxyAttempt
		if readErr == nil && record.AttemptID != "" {
			previousAttempt = record.AttemptID
		}
		if anchorErr := anchor(next.AttemptID, previousAttempt); anchorErr != nil {
			release()
			return Status{}, anchorErr
		}
		port, _, reserveErr := m.portStore().Reserve(ctx, "proxy", port)
		if reserveErr != nil {
			release()
			return Status{}, reserveErr
		}
		next.CreatedAt = time.Now().UTC()
		if writeErr := m.write("record.json", next); writeErr != nil {
			release()
			return Status{}, proxyStorageError()
		}
		release()
		record = next
		return m.spawnAttempt(ctx, record)
	}
}

func (m Manager) recoverTimedOutAttempt(ctx context.Context, attemptID string) (bool, Record, *protocol.Error) {
	release, err := tasks.Lock(ctx, m.path("transition.lock"))
	if err != nil {
		if ctx.Err() != nil {
			return false, Record{}, proxyFailure("canceled")
		}
		return false, Record{}, proxyStorageError()
	}
	defer release()
	record, err := m.readRecord()
	if err != nil {
		return false, Record{}, proxyStorageError()
	}
	if record.AttemptID != attemptID || record.State != "failed" || record.Reason != "supervisor_start_timeout" || record.EndedAt == nil {
		return false, record, nil
	}
	if !m.leaseFree(ctx) {
		if ctx.Err() != nil {
			return false, record, proxyFailure("canceled")
		}
		return false, record, proxyFailure("proxy_pending")
	}
	record.CreatedAt = time.Now().UTC()
	record.State, record.Reason, record.StartedAt, record.EndedAt = "starting", "", nil, nil
	if err := m.write("record.json", record); err != nil {
		return false, Record{}, proxyStorageError()
	}
	return true, record, nil
}

func (m Manager) awaitStart(ctx context.Context, attemptID string, createdAt time.Time) (Status, *protocol.Error) {
	timeout := m.readyTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	if age := time.Since(createdAt); !createdAt.IsZero() && age > 0 {
		timeout -= age
		if timeout < 0 {
			timeout = 0
		}
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return Status{}, proxyFailure("canceled")
		case <-deadline.C:
			status, statusErr := m.Status(ctx)
			if statusErr != nil {
				return Status{}, statusErr
			}
			if status.Running || status.State == "failed" {
				return status, nil
			}
			expired, expireErr := m.expireUnreadyAttempt(ctx, attemptID)
			if expireErr != nil {
				return Status{}, expireErr
			}
			if expired {
				status, statusErr := m.Status(ctx)
				return status, statusErr
			}
			return Status{}, proxyFailure("proxy_pending")
		case <-ticker.C:
			status, statusErr := m.Status(ctx)
			if statusErr != nil {
				return Status{}, statusErr
			}
			if status.Running || status.State == "failed" {
				return status, nil
			}
			record, readErr := m.readRecord()
			if readErr == nil && record.AttemptID == attemptID && (record.State == "starting" || record.State == "running") && record.EndedAt == nil {
				continue
			}
			return status, nil
		}
	}
}

func (m Manager) stop(ctx context.Context) (Status, *protocol.Error) {
	return m.stopAttempt(ctx, "", func(string, string) *protocol.Error { return nil })
}

func (m Manager) stopAttempt(ctx context.Context, expectedAttempt string, anchor func(string, string) *protocol.Error) (Status, *protocol.Error) {
	if ctx.Err() != nil {
		return Status{}, proxyFailure("canceled")
	}
	release, lockErr := tasks.Lock(ctx, m.path("transition.lock"))
	if lockErr != nil {
		if ctx.Err() != nil {
			return Status{}, proxyFailure("canceled")
		}
		return Status{}, proxyStorageError()
	}
	record, readErr := m.readRecord()
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		release()
		return Status{}, proxyStorageError()
	}
	if expectedAttempt != "" && (readErr != nil || record.AttemptID != expectedAttempt) {
		release()
		return m.settledStop(expectedAttempt != noProxyAttempt)
	}
	if readErr == nil && record.EndedAt == nil && (record.State == "starting" || record.State == "running") {
		if anchorErr := anchor(record.AttemptID, ""); anchorErr != nil {
			release()
			return Status{}, anchorErr
		}
	} else if expectedAttempt == "" {
		if anchorErr := anchor(noProxyAttempt, ""); anchorErr != nil {
			release()
			return Status{}, anchorErr
		}
	}
	if readErr == nil && record.State == "starting" && record.EndedAt == nil {
		now := time.Now().UTC()
		record.State, record.Reason, record.EndedAt = "stopped", "stop_requested", &now
		if writeErr := m.write("record.json", record); writeErr != nil {
			release()
			return Status{}, proxyStorageError()
		}
		release()
		if waitErr := m.waitLeaseRelease(ctx); waitErr != nil {
			return Status{}, waitErr
		}
		current, err := m.Status(ctx)
		if err != nil {
			return Status{}, err
		}
		current.Changed = true
		return current, nil
	}
	release()
	current, err := m.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if current.State == "unknown" {
		return Status{}, proxyFailure("proxy_unavailable")
	}
	if !current.Running {
		return current, nil
	}
	if _, rpcErr := m.rpc(ctx, "stop"); rpcErr != nil {
		if m.leaseFree(ctx) {
			current.State, current.Reason, current.Running = "interrupted", "supervisor_lost", false
			return current, nil
		}
		return Status{}, proxyFailure("proxy_unavailable")
	}
	wait, cancel := context.WithTimeout(ctx, m.effectiveStopTimeout())
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-wait.Done():
			if ctx.Err() != nil {
				return Status{}, proxyFailure("canceled")
			}
			return Status{}, proxyFailure("proxy_pending")
		case <-ticker.C:
			status, statusErr := m.Status(ctx)
			if statusErr != nil {
				return Status{}, statusErr
			}
			if !status.Running && status.State != "unknown" {
				if waitErr := m.waitLeaseRelease(ctx); waitErr != nil {
					return Status{}, waitErr
				}
				status, statusErr = m.Status(ctx)
				if statusErr != nil {
					return Status{}, statusErr
				}
				status.Changed = true
				return status, nil
			}
		}
	}
}

func (m Manager) settledStop(changed bool) (Status, *protocol.Error) {
	port, err := m.reservedPort()
	if err != nil {
		return Status{}, err
	}
	status := Status{State: "stopped", Port: port, Reason: "target_stopped", Changed: changed}
	if port != 0 {
		status.URL = "http://localhost:" + strconv.Itoa(port)
	}
	return status, nil
}

func (m Manager) expireUnreadyAttempt(ctx context.Context, attemptID string) (bool, *protocol.Error) {
	release, err := tasks.Lock(ctx, m.path("transition.lock"))
	if err != nil {
		if ctx.Err() != nil {
			return false, proxyFailure("canceled")
		}
		return false, proxyStorageError()
	}
	defer release()
	record, err := m.readRecord()
	if err != nil {
		return false, proxyStorageError()
	}
	if record.AttemptID != attemptID || record.State != "starting" && record.State != "running" || record.EndedAt != nil || !m.leaseFree(ctx) {
		return false, nil
	}
	now := time.Now().UTC()
	record.State, record.Reason, record.EndedAt = "failed", "supervisor_start_timeout", &now
	if err := m.write("record.json", record); err != nil {
		return false, proxyStorageError()
	}
	return true, nil
}

func (m Manager) waitLeaseRelease(ctx context.Context) *protocol.Error {
	wait, cancel := context.WithTimeout(ctx, m.effectiveStopTimeout())
	defer cancel()
	release, err := tasks.Lock(wait, m.path("lease.lock"))
	if err == nil {
		release()
		return nil
	}
	if ctx.Err() != nil {
		return proxyFailure("canceled")
	}
	return proxyFailure("proxy_pending")
}

func (m Manager) effectiveStopTimeout() time.Duration {
	if m.stopTimeout != 0 {
		return m.stopTimeout
	}
	return 6 * time.Second
}

func sameRecordSnapshot(left Record, leftErr error, right Record, rightErr error) bool {
	leftMissing, rightMissing := errors.Is(leftErr, os.ErrNotExist), errors.Is(rightErr, os.ErrNotExist)
	if leftMissing || rightMissing {
		return leftMissing && rightMissing
	}
	if leftErr != nil || rightErr != nil {
		return false
	}
	return left.AttemptID == right.AttemptID && left.RequestID == right.RequestID && left.State == right.State && left.Reason == right.Reason && left.CreatedAt.Equal(right.CreatedAt) && sameOptionalTime(left.StartedAt, right.StartedAt) && sameOptionalTime(left.EndedAt, right.EndedAt)
}

func sameOptionalTime(left, right *time.Time) bool {
	return left == nil && right == nil || left != nil && right != nil && left.Equal(*right)
}

func spawnProxy(data, attemptID string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, "__proxy-serve", data, attemptID)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func (m Manager) Serve(ctx context.Context, attemptID string) error {
	release, err := tasks.Lock(ctx, m.path("lease.lock"))
	if err != nil {
		return err
	}
	defer release()
	record, err := m.readRecord()
	if err != nil || record.AttemptID != attemptID || record.State != "starting" || record.EndedAt != nil {
		return errors.New("invalid proxy execution")
	}
	port, portErr := m.reservedPort()
	if portErr != nil || port == 0 {
		m.failAttempt(attemptID, "reservation_unavailable")
		return errors.New("proxy reservation unavailable")
	}
	listen := m.Listen
	if listen == nil {
		listen = ListenLoopback
	}
	listeners, err := listen(port)
	if err != nil {
		m.failAttempt(attemptID, "listener_bind_failed")
		return err
	}
	closeListeners := func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}
	defer closeListeners()
	controlListen := m.ControlListen
	if controlListen == nil {
		controlListen = func() (net.Listener, error) { return net.Listen("tcp4", "127.0.0.1:0") }
	}
	controlListener, err := controlListen()
	if err != nil {
		m.failAttempt(attemptID, "control_bind_failed")
		return err
	}
	defer controlListener.Close()
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		m.failAttempt(attemptID, "control_token_failed")
		return err
	}
	control := proxyControl{Address: "http://" + controlListener.Addr().String(), Token: hex.EncodeToString(tokenBytes)}
	if err = m.write("control.json", control); err != nil {
		m.failAttempt(attemptID, "control_storage_failed")
		return err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	connections := &connectionRegistry{}
	proxyServer := &http.Server{
		Handler:           NewHandler(Resolver{Ports: m.portStore()}),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       time.Minute,
	}
	serveErrors := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func(listener net.Listener) {
			serveErrors <- proxyServer.Serve(trackedListener{Listener: listener, registry: connections})
		}(listener)
	}
	controlServer := &http.Server{ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxHeaderBytes: 8192}
	controlServer.Handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		if request.Host != strings.TrimPrefix(control.Address, "http://") || request.Header.Get("Origin") != "" || request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer "+control.Token {
			http.Error(response, "Forbidden", http.StatusForbidden)
			return
		}
		if request.URL.Path != "/status" && request.URL.Path != "/stop" {
			http.NotFound(response, request)
			return
		}
		current, readErr := m.readRecord()
		if readErr != nil {
			http.Error(response, "Unavailable", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(current)
		if request.URL.Path == "/stop" {
			cancel()
		}
	})
	controlErrors := make(chan error, 1)
	go func() { controlErrors <- controlServer.Serve(controlListener) }()
	defer controlServer.Close()
	now := time.Now().UTC()
	record, changed, err := m.changeAttempt(ctx, attemptID, func(active *Record) bool {
		if active.State != "starting" || active.EndedAt != nil {
			return false
		}
		active.State, active.StartedAt, active.Reason = "running", &now, ""
		return true
	})
	if err != nil || !changed {
		m.failAttempt(attemptID, "record_storage_failed")
		if err != nil {
			return err
		}
		return errors.New("proxy start attempt is no longer active")
	}
	reason := "stop_requested"
	select {
	case <-serveCtx.Done():
	case serveErr := <-serveErrors:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			reason = "listener_failed"
		}
	case controlErr := <-controlErrors:
		if controlErr != nil && !errors.Is(controlErr, http.ErrServerClosed) {
			reason = "control_failed"
		}
	}
	_ = proxyServer.Close()
	connections.closeAll()
	now = time.Now().UTC()
	_, changed, err = m.changeAttempt(context.Background(), attemptID, func(active *Record) bool {
		if active.State != "running" || active.EndedAt != nil {
			return false
		}
		active.State, active.EndedAt, active.Reason = "stopped", &now, reason
		if reason == "listener_failed" || reason == "control_failed" {
			active.State = "failed"
		}
		return true
	})
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("proxy start attempt is no longer active")
	}
	return nil
}

func (m Manager) failAttempt(attemptID, reason string) {
	now := time.Now().UTC()
	_, _, _ = m.changeAttempt(context.Background(), attemptID, func(active *Record) bool {
		if active.State != "starting" || active.EndedAt != nil {
			return false
		}
		active.State, active.Reason, active.EndedAt = "failed", reason, &now
		return true
	})
}

func (m Manager) changeAttempt(ctx context.Context, attemptID string, change func(*Record) bool) (Record, bool, error) {
	release, err := tasks.Lock(ctx, m.path("transition.lock"))
	if err != nil {
		return Record{}, false, err
	}
	defer release()
	record, err := m.readRecord()
	if err != nil {
		return Record{}, false, err
	}
	if record.AttemptID != attemptID || !change(&record) {
		return record, false, nil
	}
	if err := m.write("record.json", record); err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

func (m Manager) rpc(ctx context.Context, action string) (Record, error) {
	var control proxyControl
	var record Record
	if tasks.ReadPrivate(m.path("control.json"), &control) != nil || len(control.Token) != 64 || !strings.HasPrefix(control.Address, "http://127.0.0.1:") {
		return record, errors.New("unavailable")
	}
	parsed, err := url.Parse(control.Address)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return record, errors.New("invalid control address")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return record, errors.New("invalid control address")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, control.Address+"/"+action, nil)
	if err != nil {
		return record, err
	}
	request.Header.Set("Authorization", "Bearer "+control.Token)
	client := http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return record, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return record, errors.New("unavailable")
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&record); err != nil {
		return record, err
	}
	return record, nil
}

func (m Manager) leaseFree(ctx context.Context) bool {
	wait, cancel := context.WithTimeout(ctx, 5*time.Millisecond)
	defer cancel()
	release, err := tasks.Lock(wait, m.path("lease.lock"))
	if err != nil {
		return false
	}
	release()
	return true
}

func validRequestID(value string) bool {
	bytes, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(bytes) == 16 && len(value) == 36 && value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-'
}
