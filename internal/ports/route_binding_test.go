package ports

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func bindingProject(t *testing.T) project.Context {
	t.Helper()
	root := privateTestRoot(t)
	if e := fixtureWriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='app'\n[ports.api]\n[proxies.api]\nhost='api.localhost'\nport='api'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestTypedBindingsPreserveNumberAndPreferConfiguredHTTPS(t *testing.T) {
	store := testPortStore(t)
	p := bindingProject(t)
	var i Instance
	if e := store.Update(context.Background(), func(st *State) (bool, *protocol.Error) {
		instance, e := st.Register(p.Profile, p.Root)
		if e != nil {
			return false, e
		}
		i = *instance
		st.Assignments = append(st.Assignments, Assignment{Instance: i, Name: "api", Port: 32100})
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := store.ConfigureListener(context.Background(), "default", "127.0.0.1:80", "127.0.0.1:443", false); e != nil {
		t.Fatal(e)
	}
	st, e := store.Read()
	if e != nil {
		t.Fatal(e)
	}
	c := project.Command{Bind: map[string]project.Binding{"PORT": {Port: "api"}, "API_URL": {Port: "api", Format: "url"}, "PUBLIC_URL": {Proxy: "api"}}}
	bind, e := st.Bind(p, c, "", nil)
	expected := map[string]string{"PORT": "32100", "API_URL": "http://127.0.0.1:32100", "PUBLIC_URL": "https://api.localhost"}
	if e != nil || !reflect.DeepEqual(bind, expected) {
		t.Fatal(bind, e)
	}
	if e := st.SetActivation(network.Activation{Route: network.RouteRef{Scope: "project", Profile: i.Profile, InstanceID: i.ID, Proxy: "api"}, Mode: "disabled"}); e != nil {
		t.Fatal(e)
	}
	bind, e = st.Bind(p, c, "", nil)
	if e != nil || bind["PUBLIC_URL"] != "https://api.localhost" {
		t.Fatal("inactive configured URL", bind, e)
	}
	st.Network.Routes = append(st.Network.Routes, network.GlobalRoute{ID: tasks.ID(), Name: "collision", Host: "api.localhost", Listener: "default", DNS: "external", Reservation: "unallocated"})
	if _, e := st.Bind(p, c, "", nil); e == nil || e.Code != "host_conflict" {
		t.Fatal(e)
	}
}
func TestPublicBindingsReportPendingAndExcludeProfileTemplates(t *testing.T) {
	p := bindingProject(t)
	st := empty()
	private := "${var.PRIVATE}"
	command := project.Command{Bind: map[string]project.Binding{"PORT": {Port: "api"}, "URL": {Proxy: "api"}, "PRIVATE": {Template: &private}}}
	env, pending := st.PublicBindings(p, command)
	if len(env) != 0 || len(pending) != 3 || len(st.Instances) != 0 {
		t.Fatal(env, pending)
	}
	if _, _, e := st.BindValidation(p, project.Command{Serve: []string{"api"}, Bind: command.Bind}, "", nil); e == nil {
		t.Fatal("private template resolved without a value")
	}
	_, pendingMap, e := st.BindValidation(p, project.Command{Serve: []string{"api"}, Bind: map[string]project.Binding{"URL": {Proxy: "api"}}}, "", nil)
	if e != nil || !pendingMap["URL"] {
		t.Fatal(pendingMap, e)
	}
}
func TestGlobalPeerEnvironmentAndNormalizedCollision(t *testing.T) {
	st := empty()
	st.UpgradeNetwork()
	for index, name := range []string{"web", "api-service"} {
		id := tasks.ID()
		reservation := GlobalReservation(id)
		st.Reservations = append(st.Reservations, Reservation{Name: reservation, Port: 32000 + index})
		st.Network.Routes = append(st.Network.Routes, network.GlobalRoute{ID: id, Name: name, Host: name + ".localhost", Reservation: reservation, Listener: "default", DNS: "auto"})
	}
	env, pending, e := st.GlobalEnvironment("web")
	if e != nil || len(pending) != 0 || env["PORT"] != "32000" || env["DEVTOOLS_API_SERVICE_URL"] != "http://127.0.0.1:32001" || env["DEVTOOLS_WEB_ROUTE_URL"] != "http://web.localhost:20200" {
		t.Fatal(env, pending, e)
	}
	st.Network.Routes = append(st.Network.Routes, network.GlobalRoute{ID: tasks.ID(), Name: "api.service"})
	if _, _, e := st.GlobalEnvironment("web"); e == nil || e.Code != "environment_key_conflict" {
		t.Fatal(e)
	}
}
