package ports

import (
	"context"
	"net"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestGlobalRunExactBusyClaimAndRelease(t *testing.T) {
	s := testPortStore(t)
	s.Probe = nil
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	id := tasks.ID()
	if e := s.Update(context.Background(), func(st *State) (bool, *protocol.Error) {
		st.UpgradeNetwork()
		if _, e := st.Reference(GlobalReservation(id), port); e != nil {
			return false, e
		}
		st.Network.Routes = append(st.Network.Routes, network.GlobalRoute{ID: id, Name: "api", Host: "api.localhost", Reservation: GlobalReservation(id), Listener: "default", DNS: "auto"})
		if e := st.EnsureDefaultListener(tasks.ID(), tasks.ID()); e != nil {
			return false, e
		}
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, release, e := s.PrepareGlobalRun(context.Background(), "api"); e == nil || e.Code != "port_in_use" {
		release()
		t.Fatalf("busy backend accepted: %v", e)
	}
	listener.Close()
	env, release, e := s.PrepareGlobalRun(context.Background(), "api")
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if env["PORT"] == "" || env["DEVTOOLS_API_ROUTE_URL"] != "http://api.localhost:20200" {
		t.Fatalf("incorrect env: %#v", env)
	}
	if _, again, e := s.PrepareGlobalRun(context.Background(), "api"); e == nil {
		again()
		t.Fatal("second owner accepted")
	}
	release()
	_, final, e := s.PrepareGlobalRun(context.Background(), "api")
	if e != nil {
		t.Fatal(e)
	}
	final()
}
