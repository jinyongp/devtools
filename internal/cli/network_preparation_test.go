package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	proxyapi "github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestAutomaticPreparationSkipsDisabledBeforeEnvironmentAndRetainsPeerPorts(t *testing.T) {
	root := privateTempDir(t)
	config := "profile='app'\n[ports.web]\n[ports.other]\nport='${UNUSED_NETWORK_PORT}'\n[proxies.app]\nhost='${MISSING_NETWORK_DOMAIN}'\nport='web'\n[proxies.unrelated]\nhost='${UNUSED_NETWORK_DOMAIN}'\nport='other'\n[commands.web]\nexec=" + fixtureExec(t, "true") + "\nserve=['web']\n[commands.worker]\nexec=" + fixtureExec(t, "true") + "\n"
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	app := testApp(t)
	manager, e := app.proxyStore()
	if e != nil {
		t.Fatal(e)
	}
	ps := managerPortStore(manager)
	var ref network.RouteRef
	if e := ps.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) {
		i, e := st.Register(p.Profile, p.Root)
		if e != nil {
			return false, e
		}
		ref = network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: i.ID, Proxy: "app"}
		return true, st.SetActivation(network.Activation{Route: ref, Mode: "disabled"})
	}); e != nil {
		t.Fatal(e)
	}
	snapshots, e := app.prepareNetwork(context.Background(), p, []string{"web", "worker"}, nil, nil, IO{}, tasks.ID(), network.PrepareAuto, false)
	if e != nil || len(snapshots) != 2 {
		t.Fatal(snapshots, e)
	}
	snapshot := snapshots["web"]
	if len(snapshot.Activations) != 1 || snapshot.Activations[0].Mode != "disabled" || len(snapshot.Listeners) != 0 || snapshot.Ports["web"] == 0 {
		t.Fatal(snapshot)
	}
	command, e := execution.ResolveConfigured(p, "web", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := app.applyNetworkSnapshot(command, snapshot); e != nil {
		t.Fatal(e)
	}
	if _, e := app.prepareNetwork(context.Background(), p, []string{"web"}, nil, nil, IO{}, tasks.ID(), network.PrepareOn, false); e == nil || e.Code != "network_environment_required" {
		t.Fatal(e)
	}
	if e := ps.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) {
		st.Network.Activations = []network.Activation{}
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := app.applyNetworkSnapshot(command, snapshot); e == nil || e.Code != "network_snapshot_changed" {
		t.Fatal(e)
	}
}

func preparationTestApp(t *testing.T) (*App, proxyapi.Manager) {
	t.Helper()
	app := testApp(t)
	dir, _ := app.dataDirectory()
	m := proxyapi.Manager{Data: filepath.Dir(dir)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := false
	m.Spawn = func(_ string, id string) error {
		started = true
		go func() { defer close(done); _ = m.Serve(ctx, id) }()
		return nil
	}
	t.Cleanup(func() {
		cancel()
		if started {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("proxy fixture did not stop")
			}
		}
	})
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", port), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	app.proxyManager = func(string) proxyapi.Manager { return m }
	return app, m
}

func TestNamedRunAutomaticallyPreparesAndHonorsDisabledAndOverrides(t *testing.T) {
	root := privateTempDir(t)
	t.Chdir(root)
	config := "profile='app'\n[ports.web]\n[proxies.route]\nhost='auto.localhost'\nport='web'\n[commands.serve]\nserve=['web']\nexec=" + fixtureExec(t, "true") + "\n"
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	app, m := preparationTestApp(t)
	for _, flags := range [][]string{{"--no-proxy"}, {"--proxy=false"}} {
		args := append([]string{"run"}, flags...)
		args = append(args, "serve")
		if code, out, err := invoke(t, app, "", args...); code != 0 {
			t.Fatal(code, out, err)
		}
	}
	status, e := m.NetworkStatus(context.Background(), "default")
	if e != nil || status["running"] == true {
		t.Fatal(status, e)
	}
	if code, _, err := invoke(t, app, "", "run", "--no-proxy", "--proxy", "serve"); code != 2 {
		t.Fatal(code, err)
	}
	if code, out, err := invoke(t, app, "", "run", "serve"); code != 0 {
		t.Fatal(code, out, err)
	}
	status, e = m.NetworkStatus(context.Background(), "default")
	if e != nil || status["running"] != true {
		t.Fatal(status, e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	st, e := managerPortStore(m).Read()
	if e != nil {
		t.Fatal(e)
	}
	i := st.Find(p.Profile, "", p.Root)
	ref := network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: i.ID, Proxy: "route"}
	hm, e := app.hostsManager(IO{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.RouteDown(context.Background(), ref, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if code, _, err := invoke(t, app, "", "run", "serve"); code != 0 {
		t.Fatal(code, err)
	}
	st, _ = managerPortStore(m).Read()
	if st.Activation(ref).Mode != "disabled" {
		t.Fatal("automatic run revived disabled route")
	}
	if code, _, err := invoke(t, app, "", "run", "--proxy", "serve"); code != 0 {
		t.Fatal(code, err)
	}
	st, _ = managerPortStore(m).Read()
	if st.Activation(ref).Mode != "active" {
		t.Fatal("forced run did not activate route")
	}
}

func TestManagedPreparationFreezesModeAndReplaysLegacyWithoutPreparation(t *testing.T) {
	root := privateTempDir(t)
	config := "profile='app'\n[ports.web]\n[proxies.route]\nhost='${MISSING_DOMAIN}'\nport='web'\n[commands.web]\nserve=['web']\nexec=" + fixtureExec(t, "true") + "\n"
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	app := testApp(t)
	q := services.Request{Action: "start", Directory: p.Root, Command: "web", RequestID: tasks.ID()}
	if e := app.prepareManagedNetwork(context.Background(), p, &q, nil, network.PrepareOff, IO{}); e != nil {
		t.Fatal(e)
	}
	if e := app.prepareManagedNetwork(context.Background(), p, &q, nil, network.PrepareAuto, IO{}); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
	legacy := services.Request{Action: "start", Directory: p.Root, Command: "web", RequestID: tasks.ID()}
	b, _ := json.Marshal(legacy)
	digest := sha256.Sum256(b)
	s, e := app.serviceStore()
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(s.Data, "processes", "receipts", legacy.RequestID+".json")
	if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
		t.Fatal(e)
	}
	if e := tasks.WritePrivate(path, map[string]any{"fingerprint": hex.EncodeToString(digest[:]), "done": true, "result": services.Result{Item: services.Record{ID: legacy.RequestID}}}); e != nil {
		t.Fatal(e)
	}
	if e := app.prepareManagedNetwork(context.Background(), p, &legacy, nil, network.PrepareAuto, IO{}); e != nil || legacy.ProxyMode {
		t.Fatal(e)
	}
	result, e := s.Apply(context.Background(), legacy)
	if e != nil || !result.Replayed {
		t.Fatal(result, e)
	}
}
