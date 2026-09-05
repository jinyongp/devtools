package ports

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestPersistentAllocationAndConcurrency(t *testing.T) {
	s := Store{Directory: filepath.Join(t.TempDir(), "ports")}
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
				_, _, e = st.Allocate(ctx, *i, "web", project.Port{}, []int{20000, 20999})
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
	got, created, e := st.Allocate(ctx, a.Instance, "web", project.Port{Port: &newPort, Strict: true}, []int{31000, 31000})
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
	s := Store{Directory: filepath.Join(t.TempDir(), "ports")}
	ctx := context.Background()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	busy := l.Addr().(*net.TCPAddr).Port
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
