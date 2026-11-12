package ports

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func testPortStore(t *testing.T) Store {
	t.Helper()
	return Store{
		Directory: filepath.Join(privateTestRoot(t), "ports"),
		Probe: func(int) (bool, *protocol.Error) {
			return true, nil
		},
	}
}

func TestManagedRegistryWaitsForExclusiveMaintenance(t *testing.T) {
	root := privateTestRoot(t)
	store := ForDataRoot(root)
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	unlock := sync.OnceFunc(release)
	defer unlock()

	change := func(state *State) (bool, *protocol.Error) {
		_, condition := state.Register("example", root)
		return condition == nil, condition
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	condition := store.Update(ctx, change)
	if condition == nil || condition.Code != "deadline_exceeded" {
		t.Fatalf("expected cancellation while waiting for maintenance, got %v", condition)
	}
	if _, err := os.Stat(store.Directory); !os.IsNotExist(err) {
		t.Fatalf("blocked update prepared registry storage: %v", err)
	}
	readContext, cancelRead := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelRead()
	if _, condition := store.ReadContext(readContext); condition == nil || condition.Code != "deadline_exceeded" {
		t.Fatalf("expected cancellation while reading through maintenance, got %v", condition)
	}

	if condition := store.UpdateHeld(context.Background(), change); condition != nil {
		t.Fatal(condition)
	}
	unlock()
	state, condition := store.Read()
	if condition != nil {
		t.Fatal(condition)
	}
	if state.Find("example", "", root) == nil {
		t.Fatal("caller-held update did not persist the registered instance")
	}
}

func TestManagedRegistryReadDoesNotPrepareOrRecoverStorage(t *testing.T) {
	ctx := context.Background()
	root := privateTestRoot(t)
	store := ForDataRoot(root)
	if state, condition := store.ReadContext(ctx); condition != nil || len(state.Instances) != 0 {
		t.Fatalf("uninitialized registry was not readable: %v", condition)
	}
	standalone := Store{Directory: store.Directory}
	if condition := standalone.Update(ctx, func(state *State) (bool, *protocol.Error) {
		_, condition := state.Register("existing", root)
		return condition == nil, condition
	}); condition != nil {
		t.Fatal(condition)
	}
	if state, condition := store.ReadContext(ctx); condition != nil || state.Find("existing", "", root) == nil {
		t.Fatalf("registry created before maintenance was not readable: %v", condition)
	}
	if _, err := os.Stat(filepath.Join(root, ".maintenance")); !os.IsNotExist(err) {
		t.Fatalf("read prepared storage: %v", err)
	}
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(root, ".maintenance", "restore-pending.json")
	err = maintenance.Write(pending, []byte(`{"version":2,"phase":"applying","transaction_id":"00000000-0000-4000-8000-000000000001"}`))
	release()
	if err != nil {
		t.Fatal(err)
	}
	state, condition := store.ReadContext(ctx)
	if state != nil || condition == nil || condition.Details["state"] != "recovery_required" {
		t.Fatalf("read ignored pending transaction: %v", condition)
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("passive query changed the pending transaction: %v", err)
	}
}

func TestRegistryV1ReadAndFirstWriteMigration(t *testing.T) {
	s := testPortStore(t)
	if err := fixtureMkdirAll(s.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	directory := privateTestRoot(t)
	instance := Instance{Profile: "test", ID: "0123456789abcdef0123456789abcdef", Alias: nil, Directory: directory}
	assignment := Assignment{Instance: instance, Name: "web", Port: 32100}
	v1, marshalErr := json.Marshal(struct {
		Version     int          `json:"version"`
		Instances   []Instance   `json:"instances"`
		Assignments []Assignment `json:"assignments"`
	}{Version: 1, Instances: []Instance{instance}, Assignments: []Assignment{assignment}})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	registry := filepath.Join(s.Directory, "registry.json")
	if err := fixtureWriteFile(registry, v1, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := s.Read()
	if err != nil || state.Version != 1 || len(state.Reservations) != 0 || !reflect.DeepEqual(state.Instances, []Instance{instance}) || !reflect.DeepEqual(state.Assignments, []Assignment{assignment}) {
		t.Fatalf("v1 read = %+v %v", state, err)
	}
	unchanged, readErr := os.ReadFile(registry)
	if readErr != nil || string(unchanged) != string(v1) {
		t.Fatalf("read migrated storage: %q %v", unchanged, readErr)
	}
	if _, changed, err := s.Reserve(context.Background(), "proxy", 32101); err != nil || !changed {
		t.Fatalf("reserve = %v %v", changed, err)
	}
	state, err = s.Read()
	if err != nil || state.Version != registryVersion || state.Reservation("proxy") == nil || state.Reservation("proxy").Port != 32101 || !reflect.DeepEqual(state.Instances, []Instance{instance}) || !reflect.DeepEqual(state.Assignments, []Assignment{assignment}) {
		t.Fatalf("v4 read = %+v %v", state, err)
	}
	var stored map[string]any
	bytes, _ := os.ReadFile(registry)
	if json.Unmarshal(bytes, &stored) != nil || stored["reservations"] == nil {
		t.Fatalf("v4 storage = %s", bytes)
	}
}

func TestReservationPersistsAndExcludesAllocator(t *testing.T) {
	s := testPortStore(t)
	ctx := context.Background()
	if port, changed, err := s.Reserve(ctx, "proxy", 32102); err != nil || !changed || port != 32102 {
		t.Fatalf("reserve = %d %v %v", port, changed, err)
	}
	if port, changed, err := s.Reserve(ctx, "proxy", 32102); err != nil || changed || port != 32102 {
		t.Fatalf("repeat = %d %v %v", port, changed, err)
	}
	err := s.Update(ctx, func(state *State) (bool, *protocol.Error) {
		instance, err := state.Register("test", privateTestRoot(t))
		if err != nil {
			return false, err
		}
		assignment, _, err := s.Allocate(ctx, state, *instance, "web", project.Port{}, []int{32102, 32103})
		if err != nil {
			return false, err
		}
		if assignment.Port != 32103 {
			t.Fatalf("reserved port allocated: %d", assignment.Port)
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Reserve(ctx, "proxy", 32103); err == nil || err.Code != "port_in_use" {
		t.Fatalf("assignment collision = %v", err)
	}
	state, err := s.Read()
	if err != nil || state.Reservation("proxy").Port != 32102 {
		t.Fatalf("failed replacement changed reservation: %+v %v", state, err)
	}
}

func TestReservationAndAssignmentRaceKeepsPortUnique(t *testing.T) {
	s := testPortStore(t)
	ctx := context.Background()
	port := 32104
	directory := privateTestRoot(t)
	start := make(chan struct{})
	results := make(chan *protocol.Error, 2)
	go func() {
		<-start
		_, _, err := s.Reserve(ctx, "proxy", port)
		results <- err
	}()
	go func() {
		<-start
		results <- s.Update(ctx, func(state *State) (bool, *protocol.Error) {
			instance, err := state.Register("test", directory)
			if err != nil {
				return false, err
			}
			_, _, err = s.Allocate(ctx, state, *instance, "web", project.Port{Port: &port, Strict: true}, []int{port, port})
			return err == nil, err
		})
	}()
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else if err.Code != "port_in_use" {
			t.Fatalf("race error = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful owners = %d", successes)
	}
	state, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	owners := 0
	for _, assignment := range state.Assignments {
		if assignment.Port == port {
			owners++
		}
	}
	for _, reservation := range state.Reservations {
		if reservation.Port == port {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("port owners = %d: %+v", owners, state)
	}
}

func TestPersistentAllocationAndConcurrency(t *testing.T) {
	s := testPortStore(t)
	ctx := context.Background()
	const count = 8
	var wg sync.WaitGroup
	errs := make(chan *protocol.Error, count)
	for n := 0; n < count; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Update(ctx, func(st *State) (bool, *protocol.Error) {
				i, e := st.Register("test", privateTestRoot(t))
				if e != nil {
					return false, e
				}
				_, _, e = s.Allocate(ctx, st, *i, "web", project.Port{}, []int{20000, 20999})
				return true, e
			})
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	st, e := s.Read()
	if e != nil || len(st.Assignments) != count {
		t.Fatal(st, e)
	}
	a := st.Assignments[0]
	newPort := 31000
	got, created, e := s.Allocate(ctx, st, a.Instance, "web", project.Port{Port: &newPort, Strict: true}, []int{31000, 31000})
	if e != nil || created || got.Port != a.Port {
		t.Fatal(got, e)
	}
	release, e := s.Claim(ctx, a.ID, []string{"web"})
	if e != nil {
		t.Fatal(e)
	}
	if e := s.Active(ctx, a.ID, "web"); e == nil || e.Code != "port_run_active" {
		t.Fatal(e)
	}
	release()
	if e := s.Active(ctx, a.ID, "web"); e != nil {
		t.Fatal(e)
	}
}
func TestRunLockSupportsFullValidPortNameRange(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef"
	short := strings.Repeat("a", 106)
	if got := runLock(id, short); got != "run-"+id+"-"+hex.EncodeToString([]byte(short))+".lock" || len(got) != 254 {
		t.Fatalf("short-name lock compatibility changed: len=%d name=%q", len(got), got)
	}
	for _, length := range []int{107, 128} {
		name := strings.Repeat("b", length)
		got := runLock(id, name)
		if len(got) > 255 {
			t.Fatalf("lock basename for %d-byte name exceeds filesystem component limit: %d", length, len(got))
		}
	}

	s := testPortStore(t)
	name := strings.Repeat("c", 128)
	release, err := s.Claim(context.Background(), id, []string{name})
	if err != nil {
		t.Fatalf("claiming maximum-length valid port name failed: %v", err)
	}
	if activeErr := s.Active(context.Background(), id, name); activeErr == nil || activeErr.Code != "port_run_active" {
		t.Fatalf("active maximum-length lock was not observed: %v", activeErr)
	}
	release()
	if activeErr := s.Active(context.Background(), id, name); activeErr != nil {
		t.Fatalf("released maximum-length lock remained active: %v", activeErr)
	}
}

func TestPrepareRollbackPreviewAndOccupied(t *testing.T) {
	root := privateTestRoot(t)
	s := testPortStore(t)
	ctx := context.Background()
	busy := 21999
	s.Probe = func(port int) (bool, *protocol.Error) {
		return port != busy, nil
	}
	p := project.Context{Profile: "app", Root: root, Ports: map[string]project.Port{"web": {}, "busy": {Port: &busy, Strict: true}}}
	c := project.Command{Exec: []string{"echo"}, Serve: []string{"web", "busy"}}
	if _, _, e := s.Prepare(ctx, p, c, "", nil, []int{22000, 22999}, false); e == nil {
		t.Fatal("occupied strict accepted")
	}
	st, err := s.Read()
	if err != nil || len(st.Assignments) != 0 || len(st.Instances) != 0 {
		t.Fatal(st, err)
	}
	c.Serve = []string{"web"}
	c.Bind = map[string]project.Binding{"PORT": {Port: "web"}}
	c.Exec = []string{"echo", "${bind.PORT}"}
	prepared, _, err := s.Prepare(ctx, p, c, "", nil, []int{22000, 22999}, true)
	if err != nil || prepared.Bind["PORT"] == "" {
		t.Fatal(err)
	}
	st, err = s.Read()
	if err != nil || len(st.Assignments) != 0 {
		t.Fatal(st, err)
	}
	if _, e := os.Stat(filepath.Join(s.Directory, "registry.json")); !os.IsNotExist(e) {
		t.Fatal("preview wrote registry")
	}
	_, release, err := s.Prepare(ctx, p, c, "", nil, []int{22000, 22999}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, _, err := s.Prepare(ctx, p, c, "", nil, []int{22000, 22999}, false); err == nil || err.Code != "port_run_active" {
		t.Fatal(err)
	}
	c.Serve = nil
	if _, _, err := s.Prepare(ctx, p, c, "", nil, []int{22000, 22999}, true); err != nil {
		t.Fatal("reference blocked by active run", err)
	}
}
