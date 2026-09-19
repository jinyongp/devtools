package proxy

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

var testPortSequence atomic.Int32

func testPort() int {
	return 40000 + int(testPortSequence.Add(1))
}

type idleListener struct {
	port   int
	closed chan struct{}
	once   sync.Once
}

func newIdleListener(port int) *idleListener {
	return &idleListener{port: port, closed: make(chan struct{})}
}

func (l *idleListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *idleListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *idleListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: l.port}
}

func (l *idleListener) isClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

func testManager(t *testing.T) Manager {
	t.Helper()
	return Manager{
		Data: t.TempDir(),
		PortProbe: func(int) (bool, *protocol.Error) {
			return true, nil
		},
		Listen: func(port int) ([]net.Listener, error) {
			return []net.Listener{newIdleListener(port)}, nil
		},
	}
}

func TestListenLoopbackRollsBackFirstBind(t *testing.T) {
	for _, bindError := range []error{syscall.EADDRINUSE, syscall.EADDRNOTAVAIL} {
		t.Run(bindError.Error(), func(t *testing.T) {
			first := newIdleListener(testPort())
			listeners, err := listenLoopback(first.port, func(network, _ string) (net.Listener, error) {
				if network == "tcp6" {
					return nil, bindError
				}
				return first, nil
			})
			if !errors.Is(err, bindError) || len(listeners) != 0 || !first.isClosed() {
				t.Fatalf("rollback = %+v %v closed=%v", listeners, err, first.isClosed())
			}
		})
	}
}

func TestConnectionRegistryClosesAcceptedConnectionBeforeHijackCallback(t *testing.T) {
	for _, trackAfterShutdown := range []bool{false, true} {
		name := "tracked_before_shutdown"
		if trackAfterShutdown {
			name = "accepted_during_shutdown"
		}
		t.Run(name, func(t *testing.T) {
			registry := &connectionRegistry{}
			server, client := net.Pipe()
			defer client.Close()
			if err := client.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if trackAfterShutdown {
				registry.closeAll()
			}
			registry.track(server)
			if !trackAfterShutdown {
				registry.closeAll()
			}
			if _, err := client.Read(make([]byte, 1)); err == nil {
				t.Fatal("tracked connection remained open")
			} else if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
				t.Fatal("tracked connection timed out instead of closing")
			}
			registry.mu.Lock()
			defer registry.mu.Unlock()
			if len(registry.connections) != 0 {
				t.Fatalf("closed connections retained = %d", len(registry.connections))
			}
		})
	}
}

func TestManagerStartStopRetryAndReservationReplacement(t *testing.T) {
	manager := testManager(t)
	serveErrors := make(chan error, 2)
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { serveErrors <- manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	firstPort := testPort()
	firstRequest := Request{Action: "start", Port: &firstPort, RequestID: "11111111-1111-4111-8111-111111111111"}
	started, err := manager.Apply(context.Background(), firstRequest)
	if err != nil || !started.Running || !started.Changed || started.Port != firstPort || started.StartedAt == nil {
		t.Fatalf("start = %+v %v", started, err)
	}
	replayed, err := manager.Apply(context.Background(), firstRequest)
	if err != nil || !replayed.Replayed || replayed.Port != firstPort {
		t.Fatalf("replay = %+v %v", replayed, err)
	}
	otherPort := testPort()
	conflictRequest := firstRequest
	conflictRequest.Port = &otherPort
	if _, err := manager.Apply(context.Background(), conflictRequest); err == nil || err.Code != "request_conflict" {
		t.Fatalf("request conflict = %v", err)
	}
	stopped, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "22222222-2222-4222-8222-222222222222"})
	if err != nil || stopped.Running || stopped.State != "stopped" || stopped.Port != firstPort {
		t.Fatalf("stop = %+v %v", stopped, err)
	}
	select {
	case serveErr := <-serveErrors:
		if serveErr != nil {
			t.Fatal(serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
	state, portErr := manager.portStore().Read()
	if portErr != nil || state.Reservation("proxy") == nil || state.Reservation("proxy").Port != firstPort {
		t.Fatalf("stopped reservation = %+v %v", state, portErr)
	}

	restarted, err := manager.Apply(context.Background(), Request{Action: "start", Port: &otherPort, RequestID: "33333333-3333-4333-8333-333333333333"})
	if err != nil || !restarted.Running || restarted.Port != otherPort {
		t.Fatalf("restart = %+v %v", restarted, err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "44444444-4444-4444-8444-444444444444"}); err != nil {
		t.Fatal(err)
	}
	select {
	case serveErr := <-serveErrors:
		if serveErr != nil {
			t.Fatal(serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("restarted supervisor did not stop")
	}
	state, portErr = manager.portStore().Read()
	if portErr != nil || state.Reservation("proxy").Port != otherPort {
		t.Fatalf("replacement reservation = %+v %v", state, portErr)
	}
}

func TestManagerPreservesReservationAfterSpawnFailure(t *testing.T) {
	var spawns atomic.Int32
	manager := testManager(t)
	manager.Spawn = func(string, string) error {
		spawns.Add(1)
		return errors.New("injected spawn failure")
	}
	port := testPort()
	request := Request{Action: "start", Port: &port, RequestID: "55555555-5555-4555-8555-555555555555"}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "proxy_start_failed" {
			t.Fatalf("failed start %d = %v", attempt, err)
		}
	}
	if spawns.Load() != 1 {
		t.Fatalf("failed start spawns = %d", spawns.Load())
	}
	status, statusErr := manager.Status(context.Background())
	if statusErr != nil || status.State != "failed" || status.Reason != "supervisor_start_failed" {
		t.Fatalf("failed status = %+v %v", status, statusErr)
	}
	state, portErr := manager.portStore().Read()
	if portErr != nil || state.Reservation("proxy") == nil || state.Reservation("proxy").Port != port {
		t.Fatalf("failed reservation = %+v %v", state, portErr)
	}
	info, statErr := os.Stat(filepath.Join(manager.Data, "proxy"))
	if statErr != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("proxy directory permissions = %v %v", info, statErr)
	}
}

func TestManagerDoesNotOverwriteLateSupervisorAfterSpawnError(t *testing.T) {
	manager := testManager(t)
	serveErrors := make(chan error, 1)
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { serveErrors <- manager.Serve(context.Background(), attemptID) }()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			record, err := manager.readRecord()
			if err == nil && record.AttemptID == attemptID && record.State == "running" {
				return errors.New("injected error after supervisor started")
			}
			time.Sleep(5 * time.Millisecond)
		}
		return errors.New("supervisor did not start")
	}
	port := testPort()
	started, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "56555555-5555-4555-8555-555555555556"})
	if err != nil || !started.Running || started.State != "running" {
		t.Fatalf("late supervisor = %+v %v", started, err)
	}
	record, readErr := manager.readRecord()
	if readErr != nil || record.State != "running" || record.EndedAt != nil {
		t.Fatalf("late supervisor record = %+v %v", record, readErr)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "57555555-5555-4555-8555-555555555557"}); err != nil {
		t.Fatal(err)
	}
	select {
	case serveErr := <-serveErrors:
		if serveErr != nil {
			t.Fatal(serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("late supervisor did not stop")
	}
}

func TestManagerReusesStartingSupervisorAfterCanceledStart(t *testing.T) {
	manager := testManager(t)
	var spawns atomic.Int32
	firstContext, cancelFirst := context.WithCancel(context.Background())
	manager.Spawn = func(_ string, attemptID string) error {
		if spawns.Add(1) == 1 {
			cancelFirst()
		}
		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = manager.Serve(context.Background(), attemptID)
		}()
		return nil
	}
	firstPort, otherPort := testPort(), testPort()
	firstRequest := Request{Action: "start", Port: &firstPort, RequestID: "53535353-5353-4353-8353-535353535353"}
	if _, err := manager.Apply(firstContext, firstRequest); err == nil || err.Code != "canceled" {
		t.Fatalf("canceled start = %v", err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "start", Port: &otherPort, RequestID: "54545454-5454-4454-8454-545454545454"}); err == nil || err.Code != "proxy_conflict" {
		t.Fatalf("different port during start = %v", err)
	}
	status, err := manager.Apply(context.Background(), Request{Action: "start", Port: &firstPort, RequestID: "55545454-5454-4454-8454-545454545455"})
	if err != nil || !status.Running || status.Port != firstPort || spawns.Load() != 1 {
		t.Fatalf("reused start = %+v %v, spawns=%d", status, err, spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "56565656-5656-4656-8656-565656565656"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerWaitsForDelayedSupervisorLease(t *testing.T) {
	manager := testManager(t)
	manager.Spawn = func(_ string, attemptID string) error {
		go func() {
			time.Sleep(2100 * time.Millisecond)
			_ = manager.Serve(context.Background(), attemptID)
		}()
		return nil
	}
	port := testPort()
	status, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "51515151-5151-4151-8151-515151515151"})
	if err != nil || !status.Running {
		t.Fatalf("delayed supervisor = %+v %v", status, err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "52525252-5252-4252-8252-525252525252"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerStopCancelsStartingAttemptAndRejectsStaleChild(t *testing.T) {
	manager := testManager(t)
	firstContext, cancelFirst := context.WithCancel(context.Background())
	var staleAttempt string
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		spawns.Add(1)
		if staleAttempt == "" {
			staleAttempt = attemptID
			cancelFirst()
			return nil
		}
		if err := manager.Serve(context.Background(), staleAttempt); err == nil {
			t.Error("stale supervisor adopted a newer start attempt")
		}
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	port := testPort()
	if _, err := manager.Apply(firstContext, Request{Action: "start", Port: &port, RequestID: "57575757-5757-4757-8757-575757575757"}); err == nil || err.Code != "canceled" {
		t.Fatalf("canceled start = %v", err)
	}
	stopped, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "58585858-5858-4858-8858-585858585858"})
	if err != nil || stopped.State != "stopped" || !stopped.Changed {
		t.Fatalf("stop starting = %+v %v", stopped, err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "57575757-5757-4757-8757-575757575757"}); err == nil || err.Code != "canceled" || spawns.Load() != 1 {
		t.Fatalf("canceled replay = %v, spawns=%d", err, spawns.Load())
	}
	started, err := manager.Apply(context.Background(), Request{Action: "start", RequestID: "59595959-5959-4959-8959-595959595959"})
	if err != nil || !started.Running || started.Port != port {
		t.Fatalf("fresh start = %+v %v", started, err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "60606060-6060-4060-8060-606060606060"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerRecoversRunningRecordWithoutStartedAt(t *testing.T) {
	manager := testManager(t)
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	if err := manager.write("record.json", Record{CreatedAt: time.Now().Add(-time.Minute), AttemptID: "lost", State: "running"}); err != nil {
		t.Fatal(err)
	}
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started, err := manager.Apply(ctx, Request{Action: "start", RequestID: "68686868-6868-4868-8868-686868686868"})
	if err != nil || !started.Running || started.Port != port {
		t.Fatalf("recovered malformed running record = %+v %v", started, err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "69696969-6969-4969-8969-696969696969"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerKeepsCommittedRunningAttemptPendingUntilControlReady(t *testing.T) {
	manager := testManager(t)
	manager.readyTimeout = 50 * time.Millisecond
	releaseLease := make(chan struct{})
	manager.Spawn = func(_ string, attemptID string) error {
		ready := make(chan struct{})
		go func() {
			release, err := tasks.Lock(context.Background(), manager.path("lease.lock"))
			if err != nil {
				t.Error(err)
				close(ready)
				return
			}
			defer release()
			now := time.Now().UTC()
			_, changed, err := manager.changeAttempt(context.Background(), attemptID, func(record *Record) bool {
				record.State, record.StartedAt = "running", &now
				return true
			})
			if err != nil || !changed {
				t.Errorf("running transition = %v %v", changed, err)
			}
			close(ready)
			<-releaseLease
		}()
		<-ready
		return nil
	}
	port := testPort()
	request := Request{Action: "start", Port: &port, RequestID: "70707070-7070-4070-8070-707070707070"}
	if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "proxy_pending" {
		t.Fatalf("unready control = %v", err)
	}
	if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "proxy_pending" {
		t.Fatalf("unready control replay = %v", err)
	}
	controlServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		record, err := manager.readRecord()
		if err != nil {
			t.Error(err)
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(response).Encode(record)
	}))
	defer controlServer.Close()
	if err := manager.write("control.json", proxyControl{Address: controlServer.URL, Token: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Apply(context.Background(), request)
	if err != nil || !status.Running {
		t.Fatalf("ready control replay = %+v %v", status, err)
	}
	close(releaseLease)
}

func TestPendingStartDoesNotAdoptReplacementAttempt(t *testing.T) {
	manager := testManager(t)
	manager.readyTimeout = 50 * time.Millisecond
	releaseFirst := make(chan struct{})
	var firstAttempt string
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		if spawns.Add(1) == 1 {
			firstAttempt = attemptID
			ready := make(chan struct{})
			go func() {
				release, err := tasks.Lock(context.Background(), manager.path("lease.lock"))
				if err != nil {
					t.Error(err)
					close(ready)
					return
				}
				now := time.Now().UTC()
				_, _, _ = manager.changeAttempt(context.Background(), attemptID, func(record *Record) bool {
					record.State, record.StartedAt = "running", &now
					return true
				})
				close(ready)
				<-releaseFirst
				release()
			}()
			<-ready
			return nil
		}
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	port := testPort()
	firstRequest := Request{Action: "start", Port: &port, RequestID: "75757575-7575-4575-8575-757575757575"}
	if _, err := manager.Apply(context.Background(), firstRequest); err == nil || err.Code != "proxy_pending" {
		t.Fatalf("first pending = %v", err)
	}
	close(releaseFirst)
	if err := manager.waitLeaseRelease(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, changed, err := manager.changeAttempt(context.Background(), firstAttempt, func(record *Record) bool {
		record.State, record.Reason, record.EndedAt = "stopped", "stop_requested", &now
		return true
	}); err != nil || !changed {
		t.Fatalf("finish first = %v %v", changed, err)
	}
	second, err := manager.Apply(context.Background(), Request{Action: "start", RequestID: "76767676-7676-4676-8676-767676767676"})
	if err != nil || !second.Running {
		t.Fatalf("replacement = %+v %v", second, err)
	}
	if _, err := manager.Apply(context.Background(), firstRequest); err == nil || err.Code != "proxy_start_failed" {
		t.Fatalf("old pending replay = %v", err)
	}
	current, statusErr := manager.Status(context.Background())
	if statusErr != nil || !current.Running || current.StartedAt == nil || second.StartedAt == nil || !current.StartedAt.Equal(*second.StartedAt) || spawns.Load() != 2 {
		t.Fatalf("replacement changed by replay = %+v %v, spawns=%d", current, statusErr, spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "77767676-7676-4676-8676-767676767677"}); err != nil {
		t.Fatal(err)
	}
}

func TestAnchoredStartRecoversFromFinalReceiptWriteFailure(t *testing.T) {
	manager := testManager(t)
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		spawns.Add(1)
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	failFinal := true
	manager.commit = func(path string, value any) error {
		if stored, ok := value.(receipt); ok && stored.Done && failFinal {
			return errors.New("injected final receipt failure")
		}
		return tasks.WritePrivate(path, value)
	}
	port := testPort()
	request := Request{Action: "start", Port: &port, RequestID: "79797979-7979-4979-8979-797979797979"}
	if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "io_error" {
		t.Fatalf("final receipt failure = %v", err)
	}
	failFinal = false
	status, err := manager.Apply(context.Background(), request)
	if err != nil || !status.Running || spawns.Load() != 1 {
		t.Fatalf("anchored recovery = %+v %v, spawns=%d", status, err, spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "80808080-8080-4080-8080-808080808080"}); err != nil {
		t.Fatal(err)
	}
}

func TestAnchoredNoOpStopDoesNotStopLaterAttemptAfterReceiptFailure(t *testing.T) {
	manager := testManager(t)
	failFinal := true
	manager.commit = func(path string, value any) error {
		if stored, ok := value.(receipt); ok && stored.Done && failFinal {
			return errors.New("injected final receipt failure")
		}
		return tasks.WritePrivate(path, value)
	}
	stopRequest := Request{Action: "stop", RequestID: "87878787-8787-4787-8787-878787878787"}
	if _, err := manager.Apply(context.Background(), stopRequest); err == nil || err.Code != "io_error" {
		t.Fatalf("no-op stop receipt failure = %v", err)
	}
	failFinal = false
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	port := testPort()
	started, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "88878787-8787-4787-8787-878787878788"})
	if err != nil || !started.Running {
		t.Fatalf("later start = %+v %v", started, err)
	}
	if settled, err := manager.Apply(context.Background(), stopRequest); err != nil || settled.State != "stopped" || settled.Changed {
		t.Fatalf("no-op stop replay = %+v %v", settled, err)
	}
	if current, err := manager.Status(context.Background()); err != nil || !current.Running {
		t.Fatalf("later attempt was stopped = %+v %v", current, err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "89898989-8989-4989-8989-898989898989"}); err != nil {
		t.Fatal(err)
	}
}

func TestStartAnchorFailureDoesNotChangeReservation(t *testing.T) {
	manager := testManager(t)
	oldPort, newPort := testPort(), testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", oldPort); err != nil {
		t.Fatal(err)
	}
	manager.commit = func(path string, value any) error {
		if stored, ok := value.(receipt); ok && stored.AttemptID != "" {
			return errors.New("injected anchor failure")
		}
		return tasks.WritePrivate(path, value)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "start", Port: &newPort, RequestID: "90909090-9090-4090-8090-909090909090"}); err == nil || err.Code != "io_error" {
		t.Fatalf("anchor failure = %v", err)
	}
	state, err := manager.portStore().Read()
	if err != nil || state.Reservation("proxy") == nil || state.Reservation("proxy").Port != oldPort {
		t.Fatalf("reservation changed before anchor = %+v %v", state, err)
	}
	if _, err := manager.readRecord(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record created before anchor: %v", err)
	}
}

func TestAnchoredStartResumesWhenRecordCommitAndTerminalReceiptFail(t *testing.T) {
	manager := testManager(t)
	failWrites := true
	manager.commit = func(path string, value any) error {
		if failWrites {
			if record, ok := value.(Record); ok && record.State == "starting" {
				return errors.New("injected record commit failure")
			}
			if stored, ok := value.(receipt); ok && stored.Done {
				return errors.New("injected terminal receipt failure")
			}
		}
		return tasks.WritePrivate(path, value)
	}
	port := testPort()
	request := Request{Action: "start", Port: &port, RequestID: "91919191-9191-4191-8191-919191919191"}
	if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "io_error" {
		t.Fatalf("intermediate commit failure = %v", err)
	}
	failWrites = false
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		spawns.Add(1)
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	started, err := manager.Apply(context.Background(), request)
	if err != nil || !started.Running || spawns.Load() != 1 {
		t.Fatalf("planned attempt recovery = %+v %v, spawns=%d", started, err, spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "92929292-9292-4292-8292-929292929292"}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryAnchorResumesTimedOutAttemptAfterCrash(t *testing.T) {
	manager := testManager(t)
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	request := Request{Action: "start", Port: &port, RequestID: "93939393-9393-4393-8393-939393939393"}
	encoded, _ := json.Marshal(request)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	endedAt := time.Now().UTC()
	record := Record{CreatedAt: endedAt.Add(-time.Second), AttemptID: "recoverable", RequestID: request.RequestID, State: "failed", Reason: "supervisor_start_timeout", EndedAt: &endedAt}
	if err := manager.write("record.json", record); err != nil {
		t.Fatal(err)
	}
	if err := manager.write(filepath.Join("receipts", request.RequestID+".json"), receipt{Fingerprint: fingerprint, AttemptID: record.AttemptID, PreviousAttempt: record.AttemptID}); err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		spawns.Add(1)
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	started, err := manager.Apply(context.Background(), request)
	if err != nil || !started.Running || !started.Changed || spawns.Load() != 1 {
		t.Fatalf("recovery anchor resume = %+v %v, spawns=%d", started, err, spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "94949494-9494-4494-8494-949494949494"}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryResumesTimedOutAttemptBeforeAnchorRewrite(t *testing.T) {
	manager := testManager(t)
	failFinal := true
	manager.commit = func(path string, value any) error {
		if stored, ok := value.(receipt); ok && stored.Done && failFinal {
			return errors.New("injected final receipt failure")
		}
		return tasks.WritePrivate(path, value)
	}
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	request := Request{Action: "start", Port: &port, RequestID: "94939393-9393-4393-8393-939393939394"}
	encoded, _ := json.Marshal(request)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	endedAt := time.Now().UTC()
	record := Record{CreatedAt: endedAt.Add(-time.Second), AttemptID: "recoverable-before-anchor", RequestID: request.RequestID, State: "failed", Reason: "supervisor_start_timeout", EndedAt: &endedAt}
	if err := manager.write("record.json", record); err != nil {
		t.Fatal(err)
	}
	if err := manager.write(filepath.Join("receipts", request.RequestID+".json"), receipt{Fingerprint: fingerprint, AttemptID: record.AttemptID}); err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		spawns.Add(1)
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "io_error" {
		t.Fatalf("recovered final receipt failure = %v", err)
	}
	failFinal = false
	started, err := manager.Apply(context.Background(), request)
	if err != nil || !started.Running || !started.Changed || spawns.Load() != 1 {
		t.Fatalf("pre-anchor recovery replay = %+v %v, spawns=%d", started, err, spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "95939393-9393-4393-8393-939393939395"}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryKeepsTimedOutAttemptPendingWhileLeaseIsBusy(t *testing.T) {
	manager := testManager(t)
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	request := Request{Action: "start", Port: &port, RequestID: "95939393-9393-4393-8393-939393939395"}
	encoded, _ := json.Marshal(request)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	endedAt := time.Now().UTC()
	record := Record{CreatedAt: endedAt.Add(-time.Second), AttemptID: "recoverable-busy-lease", RequestID: request.RequestID, State: "failed", Reason: "supervisor_start_timeout", EndedAt: &endedAt}
	if err := manager.write("record.json", record); err != nil {
		t.Fatal(err)
	}
	if err := manager.write(filepath.Join("receipts", request.RequestID+".json"), receipt{Fingerprint: fingerprint, AttemptID: record.AttemptID}); err != nil {
		t.Fatal(err)
	}
	release, lockErr := tasks.Lock(context.Background(), manager.path("lease.lock"))
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	if _, err := manager.Apply(context.Background(), request); err == nil || err.Code != "proxy_pending" {
		release()
		t.Fatalf("busy recovery = %v", err)
	}
	release()
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		spawns.Add(1)
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	started, err := manager.Apply(context.Background(), request)
	if err != nil || !started.Running || spawns.Load() != 1 {
		t.Fatalf("recovered after busy lease = %+v %v, spawns=%d", started, err, spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "96939393-9393-4393-8393-939393939396"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerFailsWhenControlServerStopsUnexpectedly(t *testing.T) {
	manager := testManager(t)
	controlListeners := make(chan net.Listener, 1)
	manager.ControlListen = func() (net.Listener, error) {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err == nil {
			controlListeners <- listener
		}
		return listener, err
	}
	serveErrors := make(chan error, 1)
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { serveErrors <- manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	port := testPort()
	started, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "96969696-9696-4696-8696-969696969696"})
	if err != nil || !started.Running {
		t.Fatalf("start = %+v %v", started, err)
	}
	if err := (<-controlListeners).Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case serveErr := <-serveErrors:
		if serveErr != nil {
			t.Fatal(serveErr)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not exit after control failure")
	}
	status, statusErr := manager.Status(context.Background())
	if statusErr != nil || status.Running || status.State != "failed" || status.Reason != "control_failed" {
		t.Fatalf("control failure status = %+v %v", status, statusErr)
	}
}

func TestManagerClosesHijackedConnectionsBeforeStopReturns(t *testing.T) {
	backendRelease := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		connection, buffer, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = buffer.Flush()
		<-backendRelease
	}))
	defer backend.Close()
	defer close(backendRelease)

	manager := testManager(t)
	directory := t.TempDir()
	writeConfig(t, directory, "app", "socket.localhost")
	alias := "main"
	register(t, manager.portStore(), "app", directory, &alias, targetPort(t, backend))
	listener, listenErr := net.Listen("tcp4", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatal(listenErr)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port
	manager.Listen = func(requested int) ([]net.Listener, error) {
		if requested != port {
			return nil, fmt.Errorf("unexpected proxy test port %d", requested)
		}
		return []net.Listener{listener}, nil
	}
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	if started, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "97979797-9797-4797-8797-979797979797"}); err != nil || !started.Running {
		t.Fatalf("start = %+v %v", started, err)
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = fmt.Fprintf(connection, "GET /socket HTTP/1.1\r\nHost: socket.localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d", response.StatusCode)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "98989898-9898-4898-8898-989898989898"}); err != nil {
		t.Fatal(err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); err == nil {
		t.Fatal("hijacked connection remained open after stop")
	} else if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
		t.Fatal("hijacked connection timed out instead of closing after stop")
	}
}

func TestManagerStartsWithFreshDeadlineAfterTransitionLockContention(t *testing.T) {
	manager := testManager(t)
	manager.readyTimeout = 100 * time.Millisecond
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	release, lockErr := tasks.Lock(context.Background(), manager.path("transition.lock"))
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	port := testPort()
	result := make(chan struct {
		status Status
		err    *protocol.Error
	}, 1)
	go func() {
		status, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "81818181-8181-4181-8181-818181818181"})
		result <- struct {
			status Status
			err    *protocol.Error
		}{status, err}
	}()
	time.Sleep(150 * time.Millisecond)
	release()
	started := <-result
	if started.err != nil || !started.status.Running {
		t.Fatalf("contended start = %+v %v", started.status, started.err)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "82828282-8282-4282-8282-828282828282"}); err != nil {
		t.Fatal(err)
	}
}

func TestPendingStopDoesNotStopReplacementAttempt(t *testing.T) {
	manager := testManager(t)
	manager.stopTimeout = 50 * time.Millisecond
	var spawns atomic.Int32
	var firstAttempt string
	manager.Spawn = func(_ string, attemptID string) error {
		if spawns.Add(1) == 1 {
			firstAttempt = attemptID
		}
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	blockEntered := make(chan struct{})
	releaseCommit := make(chan struct{})
	var blockOnce sync.Once
	manager.commit = func(path string, value any) error {
		if record, ok := value.(Record); ok && record.AttemptID == firstAttempt && record.State == "stopped" {
			blockOnce.Do(func() { close(blockEntered) })
			<-releaseCommit
		}
		return tasks.WritePrivate(path, value)
	}
	port := testPort()
	if started, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "83838383-8383-4383-8383-838383838383"}); err != nil || !started.Running {
		t.Fatalf("first start = %+v %v", started, err)
	}
	stopRequest := Request{Action: "stop", RequestID: "84848484-8484-4484-8484-848484848484"}
	if _, err := manager.Apply(context.Background(), stopRequest); err == nil || err.Code != "proxy_pending" {
		t.Fatalf("pending stop = %v", err)
	}
	<-blockEntered
	close(releaseCommit)
	if err := manager.waitLeaseRelease(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := manager.Apply(context.Background(), Request{Action: "start", RequestID: "85858585-8585-4585-8585-858585858585"})
	if err != nil || !second.Running {
		t.Fatalf("replacement start = %+v %v", second, err)
	}
	settled, err := manager.Apply(context.Background(), stopRequest)
	if err != nil || settled.State != "stopped" || spawns.Load() != 2 {
		t.Fatalf("old stop replay = %+v %v, spawns=%d", settled, err, spawns.Load())
	}
	current, statusErr := manager.Status(context.Background())
	if statusErr != nil || !current.Running || current.StartedAt == nil || second.StartedAt == nil || !current.StartedAt.Equal(*second.StartedAt) {
		t.Fatalf("replacement stopped by replay = %+v %v", current, statusErr)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "86868686-8686-4686-8686-868686868686"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerReportsStorageFailureWhileExpiringAttempt(t *testing.T) {
	manager := testManager(t)
	manager.readyTimeout = 50 * time.Millisecond
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	if err := manager.write("record.json", Record{CreatedAt: time.Now().Add(-time.Minute), AttemptID: "stale", State: "starting"}); err != nil {
		t.Fatal(err)
	}
	manager.commit = func(path string, value any) error {
		if record, ok := value.(Record); ok && record.Reason == "supervisor_start_timeout" {
			return errors.New("injected expiration write failure")
		}
		return tasks.WritePrivate(path, value)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "start", RequestID: "71717171-7171-4171-8171-717171717171"}); err == nil || err.Code != "io_error" {
		t.Fatalf("expiration storage error = %v", err)
	}
}

func TestManagerStopDoesNotClaimUnknownSupervisorStopped(t *testing.T) {
	manager := testManager(t)
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	if err := manager.write("record.json", Record{CreatedAt: time.Now().UTC(), AttemptID: "active", State: "running"}); err != nil {
		t.Fatal(err)
	}
	release, lockErr := tasks.Lock(context.Background(), manager.path("lease.lock"))
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	defer release()
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "61616161-6161-4161-8161-616161616161"}); err == nil || err.Code != "proxy_unavailable" {
		t.Fatalf("unknown stop = %v", err)
	}
}

func TestManagerExpiresStaleStartingAttemptAndAllowsNewRequest(t *testing.T) {
	manager := testManager(t)
	manager.readyTimeout = 500 * time.Millisecond
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	if err := manager.write("record.json", Record{CreatedAt: time.Now().Add(-time.Minute), AttemptID: "stale", State: "starting"}); err != nil {
		t.Fatal(err)
	}
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	startedAt := time.Now()
	started, err := manager.Apply(context.Background(), Request{Action: "start", RequestID: "62626262-6262-4262-8262-626262626262"})
	if err != nil || !started.Running || !started.Changed || started.Port != port {
		t.Fatalf("stale recovery = %+v %v", started, err)
	}
	if elapsed := time.Since(startedAt); elapsed > 200*time.Millisecond {
		t.Fatalf("stale attempt consumed a fresh timeout: %v", elapsed)
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "64646464-6464-4464-8464-646464646464"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerStopWaitsForStartingSupervisorLease(t *testing.T) {
	manager := testManager(t)
	listenEntered := make(chan struct{})
	releaseListen := make(chan struct{})
	manager.Listen = func(int) ([]net.Listener, error) {
		close(listenEntered)
		<-releaseListen
		return nil, syscall.EADDRINUSE
	}
	startContext, cancelStart := context.WithCancel(context.Background())
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		cancelStart()
		return nil
	}
	port := testPort()
	if _, err := manager.Apply(startContext, Request{Action: "start", Port: &port, RequestID: "65656565-6565-4565-8565-656565656565"}); err == nil || err.Code != "canceled" {
		t.Fatalf("canceled start = %v", err)
	}
	<-listenEntered
	stopped := make(chan *protocol.Error, 1)
	go func() {
		_, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "67676767-6767-4767-8767-676767676767"})
		stopped <- err
	}()
	select {
	case err := <-stopped:
		t.Fatalf("stop returned before lease release: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseListen)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not observe lease release")
	}
}

func TestManagerReplaysCompletePortError(t *testing.T) {
	manager := testManager(t)
	manager.PortProbe = nil
	listener, listenErr := net.Listen("tcp4", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatal(listenErr)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	request := Request{Action: "start", Port: &port, RequestID: "72727272-7272-4272-8272-727272727272"}
	_, first := manager.Apply(context.Background(), request)
	_, replay := manager.Apply(context.Background(), request)
	if first == nil || replay == nil || first.Code != "port_in_use" || !reflect.DeepEqual(first, replay) {
		t.Fatalf("error replay = %#v %#v", first, replay)
	}
}

func TestManagerMapsOperationsLockCancellation(t *testing.T) {
	manager := testManager(t)
	release, lockErr := tasks.Lock(context.Background(), manager.path("operations.lock"))
	if lockErr != nil {
		t.Fatal(lockErr)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Apply(ctx, Request{Action: "stop", RequestID: "73737373-7373-4373-8373-737373737373"}); err == nil || err.Code != "canceled" || err.ExitCode != 130 {
		t.Fatalf("operations lock cancellation = %#v", err)
	}
}

func TestManagerMapsStopCancellation(t *testing.T) {
	manager := testManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.stop(ctx); err == nil || err.Code != "canceled" || err.ExitCode != 130 {
		t.Fatalf("stop cancellation = %#v", err)
	}
}

func TestManagerMapsTransitionLockCancellation(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(Manager, context.Context) *protocol.Error
	}{
		{name: "start", run: func(manager Manager, ctx context.Context) *protocol.Error {
			_, err := manager.start(ctx, nil, "78787878-7878-4878-8878-787878787878", func(string, string) *protocol.Error { return nil })
			return err
		}},
		{name: "stop", run: func(manager Manager, ctx context.Context) *protocol.Error {
			_, err := manager.stop(ctx)
			return err
		}},
		{name: "expire", run: func(manager Manager, ctx context.Context) *protocol.Error {
			_, err := manager.expireUnreadyAttempt(ctx, "attempt")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := testManager(t)
			release, lockErr := tasks.Lock(context.Background(), manager.path("transition.lock"))
			if lockErr != nil {
				t.Fatal(lockErr)
			}
			defer release()
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan *protocol.Error, 1)
			go func() { result <- test.run(manager, ctx) }()
			time.Sleep(20 * time.Millisecond)
			cancel()
			if err := <-result; err == nil || err.Code != "canceled" || err.ExitCode != 130 {
				t.Fatalf("transition cancellation = %#v", err)
			}
		})
	}
}

func TestManagerReportsLostSupervisor(t *testing.T) {
	manager := testManager(t)
	port := testPort()
	if _, _, err := manager.portStore().Reserve(context.Background(), "proxy", port); err != nil {
		t.Fatal(err)
	}
	if err := manager.write("record.json", Record{State: "running"}); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(context.Background())
	if err != nil || status.Running || status.State != "interrupted" || status.Reason != "supervisor_lost" || status.Port != port {
		t.Fatalf("status = %+v %v", status, err)
	}
}

func TestManagerConcurrentStartSpawnsOneSupervisor(t *testing.T) {
	manager := testManager(t)
	var spawns atomic.Int32
	manager.Spawn = func(_ string, attemptID string) error {
		spawns.Add(1)
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	port := testPort()
	requests := []Request{
		{Action: "start", Port: &port, RequestID: "66666666-6666-4666-8666-666666666666"},
		{Action: "start", Port: &port, RequestID: "77777777-7777-4777-8777-777777777777"},
	}
	var wait sync.WaitGroup
	errors := make(chan *protocol.Error, len(requests))
	for _, request := range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := manager.Apply(context.Background(), request)
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if spawns.Load() != 1 {
		t.Fatalf("spawns = %d", spawns.Load())
	}
	if _, err := manager.Apply(context.Background(), Request{Action: "stop", RequestID: "88888888-8888-4888-8888-888888888888"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerPersistsListenerFailureAfterReservation(t *testing.T) {
	manager := testManager(t)
	manager.Listen = func(int) ([]net.Listener, error) { return nil, syscall.EADDRINUSE }
	serveError := make(chan error, 1)
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { serveError <- manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	port := testPort()
	if _, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "99999999-9999-4999-8999-999999999999"}); err == nil || err.Code != "proxy_start_failed" {
		t.Fatalf("listener failure = %v", err)
	}
	if err := <-serveError; !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("serve error = %v", err)
	}
	state, portErr := manager.portStore().Read()
	if portErr != nil || state.Reservation("proxy").Port != port {
		t.Fatalf("listener failure reservation = %+v %v", state, portErr)
	}
	status, statusErr := manager.Status(context.Background())
	if statusErr != nil || status.State != "failed" || status.Reason != "listener_bind_failed" || status.Port != port {
		t.Fatalf("listener status = %+v %v", status, statusErr)
	}
}

func TestManagerPreservesReservationWhenRecordCommitFails(t *testing.T) {
	manager := testManager(t)
	manager.commit = func(path string, value any) error {
		if filepath.Base(path) == "record.json" {
			return errors.New("injected record failure")
		}
		return tasks.WritePrivate(path, value)
	}
	port := testPort()
	if _, err := manager.Apply(context.Background(), Request{Action: "start", Port: &port, RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}); err == nil || err.Code != "io_error" {
		t.Fatalf("record failure = %v", err)
	}
	state, portErr := manager.portStore().Read()
	if portErr != nil || state.Reservation("proxy") == nil || state.Reservation("proxy").Port != port {
		t.Fatalf("record failure reservation = %+v %v", state, portErr)
	}
}
