package ports

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func testPortStore(t *testing.T) Store {
	t.Helper()
	return Store{
		Directory: filepath.Join(t.TempDir(), "ports"),
		Probe: func(int) (bool, *protocol.Error) {
			return true, nil
		},
	}
}

func TestRegistryV1ReadAndFirstWriteMigration(t *testing.T) {
	s := testPortStore(t)
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
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
	if err := os.WriteFile(registry, v1, 0600); err != nil {
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
	if err != nil || state.Version != 2 || state.Reservation("proxy") == nil || state.Reservation("proxy").Port != 32101 || !reflect.DeepEqual(state.Instances, []Instance{instance}) || !reflect.DeepEqual(state.Assignments, []Assignment{assignment}) {
		t.Fatalf("v2 read = %+v %v", state, err)
	}
	var stored map[string]any
	bytes, _ := os.ReadFile(registry)
	if json.Unmarshal(bytes, &stored) != nil || stored["reservations"] == nil {
		t.Fatalf("v2 storage = %s", bytes)
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
		instance, err := state.Register("test", t.TempDir())
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
	directory := t.TempDir()
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
				i, e := st.Register("test", t.TempDir())
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
func TestPrepareRollbackPreviewAndOccupied(t *testing.T) {
	root := t.TempDir()
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
